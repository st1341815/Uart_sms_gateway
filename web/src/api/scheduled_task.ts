// 定时任务配置
import apiClient from "@/api/client.ts";

export type LastRunStatus = 'unknown' | 'success' | 'failed';

export interface ScheduledTask {
    id: string;
    name: string;
    enabled: boolean;
    intervalDays: number;
    phoneNumber: string;
    content: string;
    /**
     * 发送该任务所用设备 ID。
     * 留空表示使用默认设备（按 config.yaml 中 Devices 的声明顺序取第一个）。
     * 多卡场景建议显式指定，避免短信从非预期号码（如境外漫游卡）发出。
     */
    deviceId?: string;
    createdAt?: number;
    lastRunAt?: number;
    lastMsgId?: string;
    lastRunStatus?: LastRunStatus;
}

// 定时任务 API (RESTful)
// 获取所有定时任务
export const getScheduledTasks = () => {
    return apiClient.get<ScheduledTask[]>('/scheduled-tasks');
};

// 获取单个定时任务
export const getScheduledTask = (id: string) => {
    return apiClient.get<ScheduledTask>(`/scheduled-tasks/${id}`);
};

// 创建定时任务
export const createScheduledTask = (task: Omit<ScheduledTask, 'id' | 'createdAt' | 'lastRunAt'>) => {
    return apiClient.post<ScheduledTask>('/scheduled-tasks', task);
};

// 更新定时任务
export const updateScheduledTask = (id: string, task: Omit<ScheduledTask, 'id' | 'createdAt' | 'lastRunAt'>) => {
    return apiClient.put<ScheduledTask>(`/scheduled-tasks/${id}`, task);
};

// 删除定时任务
export const deleteScheduledTask = (id: string) => {
    return apiClient.delete<{ message: string }>(`/scheduled-tasks/${id}`);
};

// 立即触发定时任务
export const triggerScheduledTask = (id: string) => {
    return apiClient.post<{ message: string }>(`/scheduled-tasks/${id}/trigger`, {});
};