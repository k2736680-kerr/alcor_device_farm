# Server 本地 Secret 挂载目录

Docker Compose 只读挂载本目录到 `/run/secrets/device-farm`。启用 Console 时，在部署机创建未纳入 Git 的 `console-users.yaml`，并在 `server.env` 设置：

```text
DEVICE_FARM_CONSOLE_USERS_FILE=/run/secrets/device-farm/console-users.yaml
```

文件只保存 Argon2id 密码哈希和 `viewer/operator/admin` 角色，不得保存明文密码、Service Token、Agent Token 或 STF Token。Linux 上将目录设为 `0750 root:65532`、文件设为 `0640 root:65532`，使非 root Server 容器可以只读访问。
