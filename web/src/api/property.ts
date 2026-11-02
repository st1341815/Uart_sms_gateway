// ==================== 通用 Property 接口 ====================

// 通用的 Property 响应类型
import apiClient from "@/api/client.ts";

export interface PropertyResponse<T> {
    id: string;
    name: string;
    value: T;
}

// 通用的获取 Property 方法
export const getProperty = async <T>(propertyId: string): Promise<T> => {
    const response = await apiClient.get<PropertyResponse<T>>(`/properties/${propertyId}`);
    return response.value;
};

// 通用的保存 Property 方法
export const saveProperty = async <T>(propertyId: string, name: string, value: T): Promise<void> => {
    await apiClient.put(`/properties/${propertyId}`, {
        name,
        value,
    });
};

// ==================== 通知渠道配置 ====================

const PROPERTY_ID_NOTIFICATION_CHANNELS = 'notification_channels';

// 通知渠道配置（通过 type 标识，不再使用独立ID）
export interface NotificationChannel {
    type: 'dingtalk' | 'wecom' | 'feishu' | 'email' | 'webhook' | 'telegram'; // 渠道类型，作为唯一标识
    enabled: boolean; // 是否启用
    config: Record<string, string | number | boolean | Record<string, unknown> | undefined>; // JSON配置，根据type不同而不同
}

// 获取通知渠道列表
export const getNotificationChannels = async (): Promise<NotificationChannel[]> => {
    const channels = await getProperty<NotificationChannel[]>(PROPERTY_ID_NOTIFICATION_CHANNELS);
    return channels || [];
};

// 保存通知渠道列表
export const saveNotificationChannels = async (channels: NotificationChannel[]): Promise<void> => {
    return saveProperty(PROPERTY_ID_NOTIFICATION_CHANNELS, '通知渠道配置', channels);
};

// 测试通知渠道（从数据库读取配置）
export const testNotificationChannel = async (type: string): Promise<{ message: string }> => {
    return await apiClient.post<{ message: string }>(`/notifications/${type}/test`);
};

// 说明：`/api/version` 返回的是后端 `internal/version.Version`，
// 它只在 make / CI 用 -ldflags 注入时才有真实值，裸 `go build` 打出来是 "dev"。
// 侧边栏展示的版本号因此改用前端构建期常量（见 `@/lib/version.ts`），
// 这里保留接口定义仅供调试（例如 curl /api/version 核对部署产物）。
export interface Version {
    version: string;
}

export const getVersion = () => {
    return apiClient.get<Version>('/version');
}