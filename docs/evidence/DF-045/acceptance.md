# DF-045 Device Farm Console iOS 设备域页面验收

## 当前状态

`pending`

验收入口：平台筛选、iOS 设备/Host/Pool/预约/健康/签名摘要、权限、安全和刷新一致性；明确不提供 iOS 人工远控，Android STF 页面必须回归。

必须保存 viewer/operator/admin 权限、跨平台错误、浏览器网络/存储/构建 Secret 扫描和真实 E4/E5 页面证据。Console 不得显示 Appium Node Endpoint、Dashboard、WDA 地址、Session Grant 或完整证书信息，也不得复用 Android STF 按钮制造伪远控。
