import apiClient from './client';
import type {Stats, Conversation, TextMessage} from './types';

// 获取统计信息
export const getStats = (): Promise<Stats> => {
    return apiClient.get('/messages/stats');
};

// 获取会话列表（按「设备 + 对方号码」分组）
export const getConversations = (): Promise<Conversation[]> => {
    return apiClient.get('/messages/conversations');
};

// 获取指定会话的所有消息
// deviceId 传空则返回「所有卡与对方的会话」；带上则只返回该卡的那条通信线
export const getConversationMessages = (peer: string, deviceId?: string): Promise<TextMessage[]> => {
    const suffix = deviceId ? `?device_id=${encodeURIComponent(deviceId)}` : '';
    return apiClient.get(`/messages/conversations/${encodeURIComponent(peer)}/messages${suffix}`);
};

// 删除单条短信
export const deleteMessage = (id: string) => {
    return apiClient.delete(`/messages/${id}`);
};

// 删除整个会话（与某个联系人在某张卡上的所有消息）
export const deleteConversation = (peer: string, deviceId?: string) => {
    const suffix = deviceId ? `?device_id=${encodeURIComponent(deviceId)}` : '';
    return apiClient.delete(`/messages/conversations/${encodeURIComponent(peer)}${suffix}`);
};

// 清空所有短信
export const clearMessages = () => {
    return apiClient.delete('/messages');
};