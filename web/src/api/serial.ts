import apiClient from "./client";
import type { 
    SendSMSRequest, 
    SetFlymodeRequest,
    DeviceStatusSummary,
    AllDevicesStatusResponse,
    DevicesListResponse,
} from "./types";

// ===== 发送短信 =====

// 发送短信（支持多设备）
export const sendSMS = (data: SendSMSRequest) => {
    return apiClient.post("/serial/sms", data);
};

// ===== 设备状态 =====

// 后端 DeviceStatus 的 JSON tag 是蛇形（phone_number / is_roaming），
// 而前端类型用的是驼峰，apiClient 是裸 fetch 不做转换 —— 不映射就会「字段永远 undefined」，
// 表现为本机号码一直显示「—」。所有设备状态入口都必须过这个函数。
interface RawDeviceStatus {
    id: string;
    name: string;
    port: string;
    connected: boolean;
    flymode: boolean;
    iccid?: string;
    imsi?: string;
    operator?: string;
    phone_number?: string;
    signal?: number;
    rsrp?: number;
    is_roaming?: boolean;
}

const normalizeDeviceStatus = (raw: RawDeviceStatus): DeviceStatusSummary => ({
    id: raw.id,
    name: raw.name,
    port: raw.port,
    connected: raw.connected,
    flymode: raw.flymode,
    iccid: raw.iccid,
    imsi: raw.imsi,
    operator: raw.operator,
    // 兼容两种命名：后端若改成驼峰也能跑，不会静默变 undefined
    phoneNumber: raw.phone_number ?? (raw as unknown as {phoneNumber?: string}).phoneNumber,
    signal: raw.signal,
    rsrp: raw.rsrp ?? (raw as unknown as {rsrp?: number}).rsrp,
    isRoaming: raw.is_roaming ?? (raw as unknown as {isRoaming?: boolean}).isRoaming,
});

// 获取所有设备状态
export const getAllDevicesStatus = async (): Promise<AllDevicesStatusResponse> => {
    const data = await apiClient.get<{ devices: RawDeviceStatus[]; total: number }>("/serial/status");
    const devices = (data.devices ?? []).map(normalizeDeviceStatus);
    return { devices, total: data.total ?? devices.length };
};

// 获取单个设备状态
export const getDeviceStatus = async (deviceId: string): Promise<DeviceStatusSummary> => {
    const raw = await apiClient.get<RawDeviceStatus>(`/devices/${deviceId}/status`);
    return normalizeDeviceStatus(raw);
};

// 获取设备状态（兼容旧 API，返回所有设备或单个）
export const getStatus = (deviceId?: string) => {
    if (deviceId) {
        return apiClient.get(`/serial/status?device_id=${deviceId}`);
    }
    return apiClient.get("/serial/status");
};

// ===== 设备列表 =====

// 获取设备列表（修复：直接返回 res）
export const getDevicesList = async (): Promise<DevicesListResponse> => {
    return await apiClient.get<DevicesListResponse>("/devices");
};

// 修改设备显示名称（SIM 卡别名，如「联通3113」）
// 名称存在服务端 properties 表，重启不丢；config.yaml 在容器内只读，改不到那里。
export const renameDevice = async (deviceId: string, name: string): Promise<{ id: string; name: string }> => {
    return await apiClient.put<{ id: string; name: string }>(`/devices/${deviceId}/name`, { name });
};

// 手工设置本机号码（SIM 卡不写 MSISDN 时补录；空串表示清除补录、回退到 SIM 读取值）
// 纯展示用途，不影响实际收发：收发用的是 SIM 卡本身。
// 注意接口返回的是蛇形 phone_number，这里转成驼峰给调用方。
export const setDevicePhoneNumber = async (
    deviceId: string,
    number: string,
): Promise<{ id: string; phoneNumber: string }> => {
    const res = await apiClient.put<{ id: string; phone_number?: string; phoneNumber?: string }>(
        `/devices/${deviceId}/phone-number`,
        { number },
    );
    return { id: res.id, phoneNumber: res.phone_number ?? res.phoneNumber ?? '' };
};

// ===== 设备控制 =====

// 设置飞行模式（支持指定设备）
export const setFlymode = (enabled: boolean, deviceId?: string) => {
    const data: SetFlymodeRequest = { enabled, device_id: deviceId };
    return apiClient.post("/serial/flymode", data);
};

// 设置指定设备飞行模式
export const setDeviceFlymode = (deviceId: string, enabled: boolean) => {
    return apiClient.post(`/devices/${deviceId}/flymode`, { enabled });
};

// 重启模块（支持指定设备）
export const rebootMcu = (deviceId?: string) => {
    if (deviceId) {
        return apiClient.post(`/devices/${deviceId}/reboot`);
    }
    return apiClient.post("/serial/reboot", { device_id: deviceId });
};

// 重启指定设备
export const rebootDevice = (deviceId: string) => {
    return apiClient.post(`/devices/${deviceId}/reboot`);
};
