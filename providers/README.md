# Device Providers

Provider只负责设备基础设施生命周期：发现、准备、健康检查、重启、回收和移除。

- `docker_emulator`复用Android Emulator和上游容器脚本；
- `usb_android`只处理USB/ADB设备发现与基础健康；
- `mock`用于无设备的契约和状态机测试。

Provider不执行App业务用例。

