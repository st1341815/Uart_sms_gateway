import apiClient from "./client";
import type { 
    SendSMSRequest, 
    SetFlymodeRequest,
    DeviceStatusSummary,
    AllDevicesStatusResponse,
    DevicesListResponse,
    DeviceStatus 
} from "./types";

// ===== 发送短信 =====

// 发送短信（支持多设备）
export const sendSMS = (data: SendSMSRequest) => {
    return apiClient.post("/serial/sms", data);
};

// ===== 设备状态 =====

// 获取所有设备状态（修复：直接返回 res，不需要 res.data）
export const getAllDevicesStatus = async (): Promise<AllDevicesStatusResponse> => {
    return await apiClient.get<AllDevicesStatusResponse>("/serial/status");
};

// 获取单个设备状态（修复：直接返回 res）
export const getDeviceStatus = async (deviceId: string): Promise<DeviceStatusSummary> => {
    return await apiClient.get<DeviceStatusSummary>(`/devices/${deviceId}/status`);
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
