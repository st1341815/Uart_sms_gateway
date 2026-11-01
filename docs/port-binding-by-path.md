# NewSMS 多设备串口绑定漂移问题修复（by-path 固定）

## 背景

在多 USB Modem（AirM2M Compo）场景下，如果在配置里直接使用 `/dev/ttyACM2`、`/dev/ttyACM5`、`/dev/ttyACM8` 这类设备名绑定串口，遇到 **USB 断开/重连** 或 **启动顺序变化** 时，内核可能会重新分配 `ttyACM*` 编号，导致：

- 短信/来电被错误归属到另一张卡对应的“设备名称”
- 通知里出现“接收设备不可能收到该短信”的现象

本次现场还观察到 `ModemManager` 在宿主机上频繁崩溃（segfault），会增加设备重连/枚举变化的概率。

## 根因

- 多个 AirM2M 设备的 `ID_SERIAL` 相同（如 `000000000001`），无法依赖 `by-id` 做稳定区分。
- `ttyACM*` 仅是动态分配的节点名，不保证跨重启/重连稳定。

## 解决方案（推荐）

### 1) 使用 `/dev/serial/by-path/...` 绑定设备

`/dev/serial/by-path` 以 **物理 USB 拓扑路径** 标识设备（例如 `pci-...-usb-0:3:1.6`），只要不换插 USB 口，该路径基本稳定。

将 `config.yaml` 中各设备的 `Port` 从 `/dev/ttyACM*` 改为 `/dev/serial/by-path/...`。

本环境映射关系：

- **移动 18055961111 (sms1)** → `/dev/serial/by-path/pci-0000:07:1b.0-usb-0:1:1.6`
- **O2 UK 漫游卡 (sms2)** → `/dev/serial/by-path/pci-0000:07:1b.0-usb-0:3:1.6`
- **联通 15105513113 (sms3)** → `/dev/serial/by-path/pci-0000:07:1b.0-usb-0:2:1.6`

> 注：绑定前可通过向不同 `ttyACM*` 端口发送设备的 `status` 指令（项目自定义协议）来核对 ICCID/IMSI/号码，从而确认对应关系。

### 2) 容器内可见性：挂载 `/dev/serial`

容器默认只映射了 `/dev/ttyACM*`，未必存在 `/dev/serial/by-path`。因此需要将宿主机的 `/dev/serial` 挂载进容器（只读即可）：

```yaml
volumes:
  - /dev/serial:/dev/serial:ro
```

这样容器内也能解析 `by-path` 到实际 `ttyACM*` 设备节点。

### 3) 降低设备枚举抖动：禁用 ModemManager

如果该主机不用于蜂窝拨号上网，建议禁用 ModemManager（避免争用/重置 modem，降低重连概率）：

```bash
sudo systemctl disable --now ModemManager.service
systemctl is-enabled ModemManager.service
systemctl is-active ModemManager.service
```

## 本次落地变更点

1. 更新运行时配置（宿主机）：`/home/unit3/newsms/config.yaml`
   - `Port` 改为 `/dev/serial/by-path/...`

2. 容器运行方式调整（NewSMS）：
   - 仍使用 **原镜像** `uart_sms_forwarder-multi:v11`
   - 增加挂载：`/dev/serial -> /dev/serial (ro)`

3. 宿主机服务调整：
   - `ModemManager.service` 已禁用并停止

## 验证方法

1. 容器内检查 `by-path` 是否存在：

```bash
docker exec NewSMS ls -l /dev/serial/by-path/
```

2. 观察下一条短信通知：
- 中国移动短信应稳定归属到 **移动1111**
- O2 UK 卡短信归属到 **O2UK3728**
- 联通短信归属到 **联通3113**

## 回滚

- 将 `config.yaml` 的 `Port` 改回 `/dev/ttyACM*`
- 移除 `/dev/serial` 挂载
- 如需恢复 ModemManager：

```bash
sudo systemctl enable --now ModemManager.service
```
