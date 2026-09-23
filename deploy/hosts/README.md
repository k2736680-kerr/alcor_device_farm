# 部署宿主机连接配置

本目录只管理部署工作站到测试/生产宿主机的 SSH 引导连接，不参与 Device Farm Server、Host Agent 或业务运行时配置。

- `.env.example` 是进入 Git 的字段模板，不包含真实密码；
- `.env` 是部署工作站本地文件，受仓库根 `.gitignore` 的 `.env` 规则保护；
- 密码只用于首次安装 SSH 公钥。公钥认证验证成功后清空两个 `PASSWORD` 字段；
- SSH 私钥保存在部署工作站的 `D:/AutoTestTools/Data/Ssh/`，不得进入仓库；
- Linux Server 的正式运行配置仍位于 `/etc/alcor-device-farm/server.env`；
- Linux Host Agent 的正式运行配置仍位于 `/etc/alcor-device-farm/host-agent.env`；
- macOS Host Agent 的正式运行配置由 `deploy/ios-host/host-agent.env.example` 生成，实际文件属于 macOS 服务账号且权限必须为 `600`。

首次配置时复制模板并填写两台机器的 IP、账号和密码：

```powershell
Copy-Item deploy/hosts/.env.example deploy/hosts/.env
```

密码认证只负责建立 SSH 密钥入口。后续测试、部署、升级和回滚统一使用 SSH 密钥与已验证的 Host 指纹，不长期依赖明文密码。
