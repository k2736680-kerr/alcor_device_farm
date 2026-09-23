#!/usr/bin/env python3
"""DF-047 真实 iOS Simulator 稳定性验收。

脚本只从环境变量读取 Service Token，不打印完整资源 ID、UDID、Session ID、
Grant 或内部 Endpoint。必须在对应 macOS Host 上运行，以便最终通过 simctl 验证
动态 Simulator 已被真实删除。
"""

import argparse
import concurrent.futures
import json
import os
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


UUID_PATTERN = re.compile(
    r"\b[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\b"
)
SECRET_PATTERN = re.compile(r"\b[A-Za-z0-9_-]{43}\b")
URL_PATTERN = re.compile(r"https?://[^\s\"'<>]+", re.IGNORECASE)
LONG_HEX_PATTERN = re.compile(r"\b[0-9A-Fa-f]{24,}\b")


def redacted(value):
    text = str(value or "")
    text = UUID_PATTERN.sub("<UDID已隐藏>", text)
    text = URL_PATTERN.sub("<内部地址已隐藏>", text)
    text = LONG_HEX_PATTERN.sub("<会话标识已隐藏>", text)
    return SECRET_PATTERN.sub("<敏感值已隐藏>", text)


def prefix(value):
    value = str(value or "")
    return value[:8] + "…" if len(value) > 8 else value


class VerificationError(RuntimeError):
    pass


class Client:
    def __init__(self, base_url, token, timeout):
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(self, method, path, body=None, idempotency_key=None):
        url = self.base_url + path
        headers = {"Authorization": "Bearer " + self.token, "Accept": "application/json"}
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        raw = None
        if body is not None:
            raw = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(url, data=raw, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                payload = response.read()
                decoded = json.loads(payload.decode("utf-8")) if payload else {}
                return response.status, decoded
        except urllib.error.HTTPError as error:
            payload = error.read()
            try:
                decoded = json.loads(payload.decode("utf-8")) if payload else {}
            except (UnicodeDecodeError, json.JSONDecodeError):
                decoded = {"error": {"message": "响应不是有效 JSON"}}
            failure = decoded.get("error") or {}
            code = failure.get("code", "HTTP_%d" % error.code)
            message = redacted(failure.get("message", "请求失败"))
            raise VerificationError("%s %s 返回 %d %s：%s" % (method, path, error.code, code, message))
        except urllib.error.URLError as error:
            raise VerificationError("%s %s 连接失败：%s" % (method, path, redacted(error.reason)))

    def data(self, method, path, body=None, idempotency_key=None):
        _, payload = self.request(method, path, body, idempotency_key)
        if payload.get("error"):
            raise VerificationError("%s %s 返回业务错误" % (method, path))
        return payload.get("data")

    def page(self, path):
        data = self.data("GET", path)
        return data.get("items", []) if isinstance(data, dict) else []


def operation_key(label):
    return "df047-%s-%s" % (label, uuid.uuid4().hex[:16])


def wait_until(description, fetch, predicate, timeout, interval=2):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = fetch()
        if predicate(last):
            return last
        time.sleep(interval)
    raise VerificationError("等待%s超时，最终状态=%s" % (description, redacted(last)))


def find_environment(client, configured_host, configured_pool):
    hosts = client.page("/api/v1/device-hosts?page_size=100")
    candidates = [
        item for item in hosts
        if item.get("status") == "online" and not item.get("draining")
        and (not configured_host or item.get("id") == configured_host)
    ]
    if len(candidates) != 1:
        raise VerificationError("必须唯一确定一台 online 且未排空的 iOS Host，实际=%d" % len(candidates))
    host = candidates[0]

    pools = client.page("/api/v1/device-pools?page_size=100")
    candidates = [
        item for item in pools
        if item.get("platform") == "ios" and item.get("status") == "active"
        and (not configured_pool or item.get("id") == configured_pool)
    ]
    if len(candidates) != 1:
        raise VerificationError("必须唯一确定一个 active iOS Pool，实际=%d" % len(candidates))
    pool = candidates[0]
    return host, pool


def find_catalog_selection(client, host_id, runtime_id, device_type_id):
    query = urllib.parse.urlencode({"host_id": host_id})
    catalog = client.data("GET", "/api/v1/ios-simulator-catalog?" + query)
    runtimes = catalog.get("runtimes", [])
    device_types = {item.get("id"): item for item in catalog.get("device_types", [])}
    for runtime in runtimes:
        if runtime_id and runtime.get("id") != runtime_id:
            continue
        for candidate in runtime.get("device_type_ids", []):
            if device_type_id and candidate != device_type_id:
                continue
            if candidate in device_types:
                return runtime.get("id"), candidate
    raise VerificationError("Host 受控目录中没有匹配的 iOS Runtime/iPhone 机型")


def get_device(client, device_id):
    return client.data("GET", "/api/v1/devices/" + device_id)


def wait_device(client, device_id, lifecycle, health=None, timeout=360):
    def matches(item):
        if item.get("lifecycle_status") != lifecycle:
            return False
        return health is None or item.get("health_status") == health

    return wait_until(
        "设备 %s 进入 %s/%s" % (prefix(device_id), lifecycle, health or "任意健康状态"),
        lambda: get_device(client, device_id), matches, timeout,
    )


def change_device(client, device_id, operation, reason, timeout=360):
    method = "DELETE" if operation == "delete" else "POST"
    suffix = {
        "start": "starts", "stop": "stops", "rebuild": "rebuilds", "delete": "",
    }[operation]
    path = "/api/v1/devices/%s%s" % (device_id, "/" + suffix if suffix else "")
    client.data(method, path, {"reason": reason}, operation_key(operation))
    target = {"start": "ready", "stop": "stopped", "rebuild": "ready", "delete": "deleted"}[operation]
    health = "healthy" if target == "ready" else None
    return wait_device(client, device_id, target, health, timeout)


def create_simulator(client, host_id, pool_id, runtime_id, device_type_id, label, timeout):
    started = time.monotonic()
    value = client.data(
        "POST", "/api/v1/ios-simulators",
        {
            "host_id": host_id,
            "pool_id": pool_id,
            "runtime_id": runtime_id,
            "device_type_id": device_type_id,
            "display_name": "DF-047 稳定性 %s" % label,
            "reason": "DF-047 真实 Simulator 稳定性验收",
        },
        operation_key("create"),
    )
    device_id = value["device_id"]
    device = wait_device(client, device_id, "ready", "healthy", timeout)
    serial = device.get("serial", "")
    if not serial or serial.startswith("pending:"):
        raise VerificationError("设备 %s ready 后仍无有效 UDID" % prefix(device_id))
    print("  创建完成 device=%s 用时=%.1fs" % (prefix(device_id), time.monotonic() - started), flush=True)
    return device


def create_reservation(client, pool_id, owner_id, timeout, lease_seconds=600):
    reservation = client.data(
        "POST", "/api/v1/device-reservations",
        {
            "pool_id": pool_id,
            "owner_type": "test_run",
            "owner_id": owner_id,
            "requested_capabilities": {"platformName": "iOS"},
            "lease_seconds": lease_seconds,
        },
        operation_key("reserve"),
    )
    reservation_id = reservation["id"]
    reservation = wait_until(
        "预约 %s 激活" % prefix(reservation_id),
        lambda: client.data("GET", "/api/v1/device-reservations/" + reservation_id),
        lambda item: item.get("status") in ("active", "failed"), timeout,
    )
    if reservation.get("status") != "active" or not reservation.get("device_id"):
        raise VerificationError("预约 %s 未激活，状态=%s" % (prefix(reservation_id), reservation.get("status")))
    return reservation


def issue_grant(client, reservation, owner_id):
    grant = client.data(
        "POST", "/api/v1/device-reservations/%s/session-grants" % reservation["id"],
        {"owner_type": "test_run", "owner_id": owner_id, "ttl_seconds": 120},
    )
    if grant.get("device_id") != reservation.get("device_id"):
        raise VerificationError("Session Grant 与 Reservation Device 不一致")
    return grant


def fence_request(method, endpoint, path, grant, body=None, timeout=360):
    url = endpoint.rstrip("/") + path
    headers = {"Authorization": "Session-Grant " + grant, "Accept": "application/json"}
    raw = None
    if body is not None:
        raw = json.dumps(body, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=raw, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = response.read()
            return response.status, json.loads(payload.decode("utf-8")) if payload else {}
    except urllib.error.HTTPError as error:
        payload = error.read()
        try:
            decoded = json.loads(payload.decode("utf-8")) if payload else {}
        except (UnicodeDecodeError, json.JSONDecodeError):
            decoded = {}
        failure = decoded.get("value") or decoded.get("error") or {}
        if not isinstance(failure, dict):
            failure = {}
        code = failure.get("error") or failure.get("code") or "HTTP_%d" % error.code
        message = failure.get("message") or decoded.get("message") or "请求失败"
        raise VerificationError(
            "Fence %s %s 返回 HTTP %d %s：%s" %
            (method, path, error.code, redacted(code), redacted(message)[:500])
        )
    except urllib.error.URLError as error:
        raise VerificationError("Fence %s %s 连接失败：%s" % (method, path, redacted(error.reason)))


def open_session(client, reservation, owner_id, timeout):
    grant = issue_grant(client, reservation, owner_id)
    udid = grant["udid"]
    payload = {
        "capabilities": {
            "alwaysMatch": {
                "platformName": "iOS",
                "appium:automationName": "XCUITest",
                "appium:udid": udid,
                "df:udids": udid,
                "appium:noReset": True,
                "df:skipReport": True,
            },
            "firstMatch": [{}],
        }
    }
    _, response = fence_request(
        "POST", grant["fence_endpoint"], "/session", grant["session_grant"], payload, timeout,
    )
    value = response.get("value") or {}
    session_id = value.get("sessionId") or response.get("sessionId")
    if not session_id:
        raise VerificationError("Appium 未返回 Session ID")
    try:
        _, source = fence_request(
            "GET", grant["fence_endpoint"], "/session/%s/source" % session_id,
            grant["session_grant"], timeout=timeout,
        )
        content = source.get("value")
        if not isinstance(content, str) or not content.strip():
            raise VerificationError("Session %s 的 /source 为空" % prefix(session_id))
    except Exception:
        try:
            fence_request(
                "DELETE", grant["fence_endpoint"], "/session/" + session_id,
                grant["session_grant"], timeout=timeout,
            )
        except Exception:
            pass
        raise
    return {
        "endpoint": grant["fence_endpoint"], "grant": grant["session_grant"],
        "session_id": session_id, "udid": udid, "source_length": len(content),
    }


def close_session(session, timeout):
    if not session:
        return
    fence_request(
        "DELETE", session["endpoint"], "/session/" + session["session_id"],
        session["grant"], timeout=timeout,
    )


def release_reservation(client, reservation, timeout):
    if not reservation:
        return
    current = client.data("GET", "/api/v1/device-reservations/" + reservation["id"])
    if current.get("status") not in ("pending", "active"):
        return
    client.data(
        "POST", "/api/v1/device-reservations/%s/releases" % reservation["id"],
        {"reason": "DF-047 当前循环完成", "force": False},
        operation_key("release"),
    )
    wait_device(client, reservation["device_id"], "ready", "healthy", timeout)


def assert_simulator_absent(udid):
    process = subprocess.run(
        ["/usr/bin/xcrun", "simctl", "list", "devices", "-j"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30,
    )
    if process.returncode != 0:
        raise VerificationError("simctl list 执行失败")
    payload = json.loads(process.stdout)
    for devices in payload.get("devices", {}).values():
        if any(item.get("udid") == udid for item in devices):
            raise VerificationError("已删除 Simulator 仍残留在 CoreSimulator")


def pool_update_body(pool, max_concurrency, enabled=None, reason=""):
    if enabled is None:
        enabled = pool["status"] == "active"
    return {
        "name": pool["name"],
        "platform": pool["platform"],
        "default_lease_seconds": pool["default_lease_seconds"],
        "max_lease_seconds": pool["max_lease_seconds"],
        "max_concurrency": max_concurrency,
        "total_target": pool["total_target"],
        "min_ready": pool["min_ready"],
        "enabled": enabled,
        "reason": reason,
    }


def inject_discovery_node_hang():
    uid = os.getuid()
    label = "com.alcor.device-farm.appium-node"
    printed = subprocess.run(
        ["/bin/launchctl", "print", "gui/%d/%s" % (uid, label)],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10,
    )
    if printed.returncode != 0:
        raise VerificationError("无法读取 Appium Device Farm Node 服务状态")
    match = re.search(r"^\s*pid\s*=\s*(\d+)\s*$", printed.stdout, re.MULTILINE)
    if not match:
        raise VerificationError("Appium Device Farm Node 服务没有运行 PID")
    children = subprocess.run(
        ["/usr/bin/pgrep", "-P", match.group(1)],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10,
    )
    child_ids = [line.strip() for line in children.stdout.splitlines() if line.strip().isdigit()]
    if children.returncode != 0 or not child_ids:
        raise VerificationError("Appium Device Farm Node 没有可注入故障的子进程")
    os.kill(int(child_ids[0]), signal.SIGSTOP)
    print("  已在 /source 成功后注入 Node 接口挂死故障", flush=True)


def run_one_cycle(client, environment, label, timeout, inject_node_hang=False):
    host_id, pool_id, runtime_id, device_type_id = environment
    device = None
    reservation = None
    session = None
    started = time.monotonic()
    try:
        device = create_simulator(client, host_id, pool_id, runtime_id, device_type_id, label, timeout)
        owner_id = "df047_%s_%s" % (label.replace("-", "_"), uuid.uuid4().hex[:12])
        reservation = create_reservation(client, pool_id, owner_id, timeout)
        if reservation["device_id"] != device["id"]:
            raise VerificationError("循环预约未分配到刚创建的唯一 ready Simulator")
        session = open_session(client, reservation, owner_id, timeout)
        print("  Session 与 /source 通过，source_chars=%d" % session["source_length"], flush=True)
        if inject_node_hang:
            inject_discovery_node_hang()
        close_session(session, timeout)
        session = None
        release_reservation(client, reservation, timeout)
        reservation = None
        change_device(client, device["id"], "rebuild", "DF-047 循环重建验证", timeout)
        udid = device["serial"]
        change_device(client, device["id"], "delete", "DF-047 循环清理", timeout)
        assert_simulator_absent(udid)
        print("  循环 %s 通过，用时=%.1fs" % (label, time.monotonic() - started), flush=True)
    finally:
        if session:
            try:
                close_session(session, timeout)
            except Exception:
                pass
        if reservation:
            try:
                release_reservation(client, reservation, timeout)
            except Exception:
                pass
        if device:
            try:
                current = get_device(client, device["id"])
                if current.get("lifecycle_status") in ("ready", "stopped", "quarantined"):
                    change_device(client, device["id"], "delete", "DF-047 过期回收失败清理", timeout)
            except Exception:
                pass
        if reservation:
            try:
                release_reservation(client, reservation, timeout)
            except Exception:
                pass
        if device:
            try:
                current = get_device(client, device["id"])
                if current.get("lifecycle_status") in ("ready", "stopped", "quarantined"):
                    change_device(client, device["id"], "delete", "DF-047 失败清理", timeout)
            except Exception:
                pass


def run_parallel_check(client, environment, pool, timeout):
    host_id, pool_id, runtime_id, device_type_id = environment
    devices = []
    reservations = []
    sessions = []
    original_concurrency = pool["max_concurrency"]
    try:
        devices.append(create_simulator(client, host_id, pool_id, runtime_id, device_type_id, "parallel-a", timeout))
        devices.append(create_simulator(client, host_id, pool_id, runtime_id, device_type_id, "parallel-b", timeout))
        current_pool = client.data("GET", "/api/v1/device-pools/" + pool_id)
        if current_pool["max_concurrency"] < 2:
            client.data("PUT", "/api/v1/device-pools/" + pool_id, pool_update_body(current_pool, 2))
        owners = ["df047_parallel_a_" + uuid.uuid4().hex[:8], "df047_parallel_b_" + uuid.uuid4().hex[:8]]
        reservations = [create_reservation(client, pool_id, owners[index], timeout) for index in range(2)]
        if len({item["device_id"] for item in reservations}) != 2:
            raise VerificationError("两条并发 Reservation 分配到了同一 Device")
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            futures = [
                executor.submit(open_session, client, reservations[index], owners[index], timeout)
                for index in range(2)
            ]
            errors = []
            for future in concurrent.futures.as_completed(futures):
                try:
                    sessions.append(future.result())
                except Exception as error:
                    errors.append(error)
            if errors:
                raise errors[0]
        if len({item["session_id"] for item in sessions}) != 2 or len({item["udid"] for item in sessions}) != 2:
            raise VerificationError("并发 Session 或 UDID 发生串联")
        print("  两台不同 Simulator 的 Reservation/Session/source 并发通过", flush=True)
        for session in sessions:
            close_session(session, timeout)
        sessions = []
        for reservation in reservations:
            release_reservation(client, reservation, timeout)
        reservations = []
        for device in devices:
            udid = device["serial"]
            change_device(client, device["id"], "delete", "DF-047 并发验收清理", timeout)
            assert_simulator_absent(udid)
        devices = []
    finally:
        for session in sessions:
            try:
                close_session(session, timeout)
            except Exception:
                pass
        for reservation in reservations:
            try:
                release_reservation(client, reservation, timeout)
            except Exception:
                pass
        for device in devices:
            try:
                current = get_device(client, device["id"])
                if current.get("lifecycle_status") in ("ready", "stopped", "quarantined"):
                    change_device(client, device["id"], "delete", "DF-047 并发失败清理", timeout)
            except Exception:
                pass
        current_pool = client.data("GET", "/api/v1/device-pools/" + pool_id)
        if current_pool["max_concurrency"] != original_concurrency:
            client.data(
                "PUT", "/api/v1/device-pools/" + pool_id,
                pool_update_body(current_pool, original_concurrency),
            )


def run_expiry_check(client, environment, timeout):
    host_id, pool_id, runtime_id, device_type_id = environment
    device = None
    reservation = None
    session = None
    started = time.monotonic()
    try:
        device = create_simulator(
            client, host_id, pool_id, runtime_id, device_type_id, "expiry", timeout,
        )
        owner_id = "df047_expiry_" + uuid.uuid4().hex[:12]
        reservation = create_reservation(client, pool_id, owner_id, timeout, lease_seconds=60)
        if reservation["device_id"] != device["id"]:
            raise VerificationError("过期回收预约未分配到当前 Simulator")
        session = open_session(client, reservation, owner_id, timeout)
        print("  过期回收 Session 与 /source 通过，等待 Reaper 自然回收", flush=True)
        expired = wait_until(
            "预约自然过期",
            lambda: client.data("GET", "/api/v1/device-reservations/" + reservation["id"]),
            lambda item: item.get("status") in ("expired", "failed", "released", "force_released"),
            timeout,
        )
        if expired.get("status") != "expired":
            raise VerificationError("过期回收预约终态不是 expired：%s" % expired.get("status"))
        wait_device(client, device["id"], "ready", "healthy", timeout)
        session = None
        reservation = None
        udid = device["serial"]
        change_device(client, device["id"], "delete", "DF-047 过期回收清理", timeout)
        assert_simulator_absent(udid)
        device = None
        print("  Reaper 先关闭 Session/WDA 后完成 expired 回收，用时=%.1fs" %
              (time.monotonic() - started), flush=True)
    finally:
        if session:
            try:
                close_session(session, timeout)
            except Exception:
                pass


def expect_conflict(description, action):
    try:
        action()
    except VerificationError as error:
        if "返回 409" in str(error):
            return
        raise VerificationError("%s返回了非预期错误：%s" % (description, redacted(error)))
    raise VerificationError("%s未被拒绝" % description)


def run_drain_check(client, environment, timeout):
    host_id, pool_id, runtime_id, device_type_id = environment
    before = [
        item for item in client.page("/api/v1/devices?platform=ios&page_size=100")
        if item.get("lifecycle_status") != "deleted"
    ]
    host_drained = False
    pool_disabled = False
    try:
        client.data(
            "POST", "/api/v1/device-hosts/%s/drains" % host_id,
            {"reason": "DF-047 iOS Host 排空验收"}, operation_key("drain-host"),
        )
        host_drained = True
        pool = client.data("GET", "/api/v1/device-pools/" + pool_id)
        client.data(
            "PUT", "/api/v1/device-pools/" + pool_id,
            pool_update_body(
                pool, pool["max_concurrency"], enabled=False,
                reason="DF-047 iOS Pool 禁用验收",
            ),
        )
        pool_disabled = True
        expect_conflict(
            "排空期间创建 Simulator",
            lambda: client.data(
                "POST", "/api/v1/ios-simulators",
                {
                    "host_id": host_id, "pool_id": pool_id,
                    "runtime_id": runtime_id, "device_type_id": device_type_id,
                    "display_name": "DF-047 排空拒绝验证",
                    "reason": "DF-047 排空期间禁止新建",
                },
                operation_key("drain-create"),
            ),
        )
        expect_conflict(
            "禁用 Pool 期间创建 Reservation",
            lambda: client.data(
                "POST", "/api/v1/device-reservations",
                {
                    "pool_id": pool_id, "owner_type": "test_run",
                    "owner_id": "df047_drain_" + uuid.uuid4().hex[:12],
                    "requested_capabilities": {"platformName": "iOS"},
                    "lease_seconds": 60,
                },
                operation_key("drain-reserve"),
            ),
        )
    finally:
        if pool_disabled:
            pool = client.data("GET", "/api/v1/device-pools/" + pool_id)
            client.data(
                "PUT", "/api/v1/device-pools/" + pool_id,
                pool_update_body(
                    pool, pool["max_concurrency"], enabled=True,
                    reason="DF-047 iOS Pool 禁用验收结束",
                ),
            )
        if host_drained:
            client.data(
                "DELETE", "/api/v1/device-hosts/%s/drains" % host_id,
                {"reason": "DF-047 iOS Host 排空验收结束"},
                operation_key("undrain-host"),
            )
    wait_until(
        "Host 解除排空后恢复 online",
        lambda: client.data("GET", "/api/v1/device-hosts/" + host_id),
        lambda item: item.get("status") == "online" and not item.get("draining"), timeout,
    )
    after = [
        item for item in client.page("/api/v1/devices?platform=ios&page_size=100")
        if item.get("lifecycle_status") != "deleted"
    ]
    if len(after) != len(before):
        raise VerificationError("排空/禁用期间产生了新的 iOS Device")
    print("  Host 排空和 Pool 禁用均阻止新建，恢复后重新可调度", flush=True)


def direct_appium_request(method, path, body=None, timeout=360):
    raw_endpoint = os.environ.get("DEVICE_FARM_IOS_DIRECT_APPIUM_URL", "http://127.0.0.1:4723")
    parsed = urllib.parse.urlparse(raw_endpoint)
    if parsed.scheme != "http" or parsed.hostname not in ("127.0.0.1", "localhost", "::1"):
        raise VerificationError("旁路漂移故障注入只允许连接本机回环 Appium")
    url = raw_endpoint.rstrip("/") + path
    raw = None
    headers = {"Accept": "application/json"}
    if body is not None:
        raw = json.dumps(body, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=raw, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = response.read()
            return json.loads(payload.decode("utf-8")) if payload else {}
    except urllib.error.HTTPError as error:
        payload = error.read()
        raise VerificationError(
            "旁路 Appium %s 返回 %d：%s" %
            (method, error.code, redacted(payload.decode("utf-8", errors="replace"))[:500])
        )
    except urllib.error.URLError as error:
        raise VerificationError("旁路 Appium 连接失败：%s" % redacted(error.reason))


def run_drift_check(client, environment, timeout):
    host_id, pool_id, runtime_id, device_type_id = environment
    device = None
    session_id = None
    try:
        device = create_simulator(
            client, host_id, pool_id, runtime_id, device_type_id, "drift", timeout,
        )
        response = direct_appium_request(
            "POST", "/session",
            {
                "capabilities": {
                    "alwaysMatch": {
                        "platformName": "iOS", "appium:automationName": "XCUITest",
                        "appium:udid": device["serial"], "df:udids": device["serial"],
                        "appium:noReset": True, "df:skipReport": True,
                    },
                    "firstMatch": [{}],
                }
            },
            timeout,
        )
        value = response.get("value") or {}
        session_id = value.get("sessionId") or response.get("sessionId")
        if not session_id:
            raise VerificationError("旁路 Appium 未返回 Session ID")
        wait_until(
            "无 Reservation 的 provider busy 被隔离",
            lambda: get_device(client, device["id"]),
            lambda item: item.get("lifecycle_status") == "quarantined" and
                         item.get("health_status") in ("degraded", "unhealthy") and
                         item.get("health_reason") == "IOS_PROVIDER_BUSY_WITHOUT_RESERVATION",
            timeout,
        )
        direct_appium_request("DELETE", "/session/" + session_id, timeout=timeout)
        session_id = None
        time.sleep(5)
        udid = device["serial"]
        change_device(client, device["id"], "delete", "DF-047 旁路漂移隔离清理", timeout)
        assert_simulator_absent(udid)
        device = None
        print("  无 Reservation 的旁路 Appium busy 已隔离并通过正式链路清理", flush=True)
    finally:
        if session_id:
            try:
                direct_appium_request("DELETE", "/session/" + session_id, timeout=timeout)
            except Exception:
                pass
        if device:
            try:
                current = get_device(client, device["id"])
                if current.get("lifecycle_status") in ("ready", "stopped", "quarantined"):
                    change_device(client, device["id"], "delete", "DF-047 旁路漂移失败清理", timeout)
            except Exception:
                pass


def main():
    parser = argparse.ArgumentParser(description="DF-047 iOS Simulator 真实稳定性验收")
    parser.add_argument("--iterations", type=int, default=50)
    parser.add_argument("--timeout", type=int, default=360)
    parser.add_argument("--skip-parallel", action="store_true")
    parser.add_argument("--inject-node-hang-after-source", action="store_true")
    parser.add_argument("--expiry-check", action="store_true")
    parser.add_argument("--drain-check", action="store_true")
    parser.add_argument("--drift-check", action="store_true")
    parser.add_argument("--host-id", default=os.environ.get("DEVICE_FARM_IOS_ACCEPTANCE_HOST_ID", ""))
    parser.add_argument("--pool-id", default=os.environ.get("DEVICE_FARM_IOS_ACCEPTANCE_POOL_ID", ""))
    parser.add_argument("--runtime-id", default=os.environ.get("DEVICE_FARM_IOS_ACCEPTANCE_RUNTIME_ID", ""))
    parser.add_argument("--device-type-id", default=os.environ.get("DEVICE_FARM_IOS_ACCEPTANCE_DEVICE_TYPE_ID", ""))
    args = parser.parse_args()
    if args.iterations < 1 or args.iterations > 200:
        raise VerificationError("iterations 必须在 1～200 之间")
    if args.inject_node_hang_after_source and (args.iterations != 1 or not args.skip_parallel):
        raise VerificationError("Node 挂死故障注入只允许与 --iterations 1 --skip-parallel 一起使用")
    token = os.environ.get("DEVICE_FARM_SECURITY_SERVICE_TOKEN", "")
    if len(token) < 16:
        raise VerificationError("缺少 DEVICE_FARM_SECURITY_SERVICE_TOKEN")
    base_url = os.environ.get("DEVICE_FARM_IOS_ACCEPTANCE_SERVER_URL", "http://127.0.0.1:18080")
    client = Client(base_url, token, args.timeout)
    host, pool = find_environment(client, args.host_id, args.pool_id)
    runtime_id, device_type_id = find_catalog_selection(
        client, host["id"], args.runtime_id, args.device_type_id,
    )
    environment = (host["id"], pool["id"], runtime_id, device_type_id)
    print(
        "DF-047 环境 host=%s pool=%s，iterations=%d" %
        (prefix(host["id"]), prefix(pool["id"]), args.iterations), flush=True,
    )

    parked_device = None
    devices = client.page("/api/v1/devices?platform=ios&page_size=100")
    ready = [
        item for item in devices
        if item.get("host_id") == host["id"] and item.get("pool_id") == pool["id"]
        and item.get("lifecycle_status") == "ready" and item.get("health_status") == "healthy"
    ]
    if len(ready) > 1:
        raise VerificationError("开始前存在多台 ready iOS Device，无法保证循环固定目标")
    if ready:
        parked_device = ready[0]
        print("临时停止基线 Simulator %s，避免循环错配" % prefix(parked_device["id"]), flush=True)
        change_device(client, parked_device["id"], "stop", "DF-047 稳定性期间临时停放", args.timeout)

    started = time.monotonic()
    completed = 0
    try:
        if not args.skip_parallel:
            print("开始两台动态 Simulator 并发隔离验收", flush=True)
            run_parallel_check(client, environment, pool, args.timeout)
        for index in range(1, args.iterations + 1):
            print("开始循环 %d/%d" % (index, args.iterations), flush=True)
            run_one_cycle(
                client, environment, "%03d" % index, args.timeout,
                inject_node_hang=args.inject_node_hang_after_source,
            )
            completed = index
        if args.expiry_check:
            print("开始 Reservation 带活跃 Session 自然过期验收", flush=True)
            run_expiry_check(client, environment, args.timeout)
        if args.drain_check:
            print("开始 iOS Host 排空与 Pool 禁用验收", flush=True)
            run_drain_check(client, environment, args.timeout)
        if args.drift_check:
            print("开始无 Reservation 的 provider busy 漂移验收", flush=True)
            run_drift_check(client, environment, args.timeout)
    finally:
        if parked_device:
            current = get_device(client, parked_device["id"])
            if current.get("lifecycle_status") == "stopped":
                change_device(client, parked_device["id"], "start", "DF-047 稳定性结束恢复基线设备", args.timeout)

    result = {
        "status": "passed",
        "parallel_check": not args.skip_parallel,
        "expiry_check": args.expiry_check,
        "drain_check": args.drain_check,
        "drift_check": args.drift_check,
        "completed_iterations": completed,
        "double_allocation": 0,
        "cross_device_session": 0,
        "core_simulator_residue": 0,
        "elapsed_seconds": round(time.monotonic() - started, 1),
    }
    print("DF047_RESULT=" + json.dumps(result, ensure_ascii=False, sort_keys=True), flush=True)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("DF-047 验收被中断", file=sys.stderr)
        sys.exit(130)
    except Exception as error:
        print("DF-047 验收失败：" + redacted(error), file=sys.stderr)
        sys.exit(1)
