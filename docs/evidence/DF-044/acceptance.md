# DF-044 iOS 真机、WDA 签名与健康接入验收

## 当前状态

`pending`

验收入口：E5 实际 iPhone 的配对/信任、Developer Mode、UI Automation、WDA 签名、明确 UDID Session、故障分类、轮换恢复和 20 次稳定性。证据不得包含 Apple Secret。

必须保存设备资产和 UDID 脱敏摘要、iOS/Xcode/WDA 兼容性、签名有效期摘要、成功与失败 Session、取消/超时/过期清理和 20 次循环结果。未信任、Developer Mode 关闭、Profile 过期或 WDA 失败必须阻止调度，不能回退选择其他设备。
