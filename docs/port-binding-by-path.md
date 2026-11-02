# NewSMS 多设备串口绑定漂移问题修复（by-path 固定）

## 背景

在多 USB Modem（AirM2M Compo）场景下，如果在配置里直接使用 `/dev/ttyACM2`、`/dev/ttyACM5` 这类设备名绑定串口，遇到 **USB 断开/重连** 或 **启动顺序变化** 时，内核可能会重新分配 `ttyACM*` 编号，导致：

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

本环境映射关系（号码已脱敏）：

- **卡1 / sms1**（境内卡）→ `/dev/serial/by-path/pci-0000:07:1b.0-usb-0:2:1.6`
- **卡2 / sms2**（境外漫游卡）→ `/dev/serial/by-path/pci-0000:07:1b.0-usb-0:3:1.6`

> 历史说明：曾有一张第三张卡（sms3，`usb-0:1:1.6`），**已撤下不再使用**。
> 当前只保留上述 2 张卡，设备 ID 顺序为 sms1、sms2。

> 注：绑定前可通过向不同 `ttyACM*` 端口发送设备的 `status` 指令（项目自定义协议）来核对 ICCID/IMSI/号码，从而确认对应关系。
> **核对结果写本地 `config.yaml`，不要写进仓库**（本仓库是公开的）。

### 2) 容器内可见性：挂载 `/dev/serial`

容器默认只映射了 `/dev/ttyACM*`，未必存在 `/dev/serial/by-path`。因此需要将宿主机的 `/dev/serial` 挂载进容器（只读即可）：

```yaml
volumes:
  - /dev/serial:/dev/serial:ro
```

这样容器内也能解析 `by-path` 到实际 `ttyACM*` 设备节点。

### 3) 降低设备枚举抖动：禁用 ModemManager

如果该主机不用于蜂窝拨号上网，建议禁用 ModemManager（避免争用/重置 modem，降低重连概率）。

**方式 A：在宿主机上关掉服务**

```bash
sudo systemctl disable --now ModemManager.service
systemctl is-enabled ModemManager.service
systemctl is-active ModemManager.service
```

**方式 B：只让 ModemManager 忽略这几种设备（更稳妥，不影响其它用途）**

```bash
# /etc/udev/rules.d/99-airm2m-modemmanager-ignore.rules
# 让 ModemManager 完全无视合宙/SimTech 模组（拔插时它最爱去 reset）
SUBSYSTEM=="usb", ATTR{idVendor}=="2ecc", ENV{ID_MM_DEVICE_IGNORE}="1"
SUBSYSTEM=="tty", ATTRS{idVendor}=="2ecc", ENV{ID_MM_DEVICE_IGNORE}="1"
```

```bash
sudo udevadm control --reload-rules && sudo udevadm trigger
```

### 4) 用 udev 生成固定软链（by-path 的升级版，换 PCI 槽也不漂移）

`by-path` 依赖物理 USB 拓扑，主机换 PCI 槽 / 换 USB 口就变。更彻底的做法是让 udev 按
「厂商 + 产品 + 端口」生成固定名字，容器里统一用 `/dev/sms1`、`/dev/sms2`：

```bash
# 先看每个模块的稳定属性
udevadm info -a -n /dev/ttyACM2 | grep -E 'idVendor|idProduct|KERNELS|bInterfaceNumber'
```

```bash
# /etc/udev/rules.d/99-sms-modem.rules
# 一个 AirM2M 模组会暴露多个功能口，串口那个通常是 bInterfaceNumber 06
# KERNELS 用「USB 端口路径」区分同型号的两张卡
SUBSYSTEM=="tty", ATTRS{idVendor}=="2ecc", ATTRS{idProduct}=="3012", \
  KERNELS=="1-2:1.6", SYMLINK+="sms1", GROUP="dialout", MODE="0660"
SUBSYSTEM=="tty", ATTRS{idVendor}=="2ecc", ATTRS{idProduct}=="3012", \
  KERNELS=="1-3:1.6", SYMLINK+="sms2", GROUP="dialout", MODE="0660"
```

```bash
sudo udevadm control --reload-rules && sudo udevadm trigger
ls -l /dev/sms1 /dev/sms2        # 应指向 ttyACM*
```

然后 `config.yaml` 的 `Port` 直接写 `/dev/sms1`，compose 的 `devices` 也改成：

```yaml
    devices:
      - /dev/sms1:/dev/sms1
      - /dev/sms2:/dev/sms2
```

好处：① 不再需要 `privileged: true`；② 不再依赖 `/dev/serial` 挂载（软链由 udev 直接建在 `/dev` 下）；
③ 换 PCI 槽时只改 `KERNELS` 一处。

## 本次落地变更点

1. 更新运行时配置（宿主机）：`/opt/newsms/config.yaml`
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

2. 在 Web 界面的消息列表里确认每条短信的设备标签是否正确（短信必须显式指定 device_id 才能发出）：
- 卡1（sms1）的短信应稳定归属到 **sms1**
- 卡2（sms2）的短信应稳定归属到 **sms2**

## 回滚

- 将 `config.yaml` 的 `Port` 改回 `/dev/ttyACM*`
- 移除 `/dev/serial` 挂载
- 如需恢复 ModemManager：

```bash
sudo systemctl enable --now ModemManager.service
```
