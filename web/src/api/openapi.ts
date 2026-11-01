import apiClient from './client';

export interface APIKey {
    id: string;
    name: string;
    accessKey: string;
    secretHint: string;
    deviceId: string;
    allowedIps: string;
    rateLimitPerMinute: number;
    enabled: boolean;
    createdAt: number;
    updatedAt: number;
    lastUsedAt: number;
}

export interface SMSTemplate {
    id: string;
    code: string;
    name: string;
    content: string;
    description: string;
    enabled: boolean;
    params: string[];
    createdAt: number;
    updatedAt: number;
}

export interface APISendLog {
    id: string;
    keyId: string;
    accessKey: string;
    deviceId: string;
    phone: string;
    templateId: string;
    content: string;
    status: 'accepted' | 'rejected' | 'failed';
    reason: string;
    messageId: string;
    clientIp: string;
    createdAt: number;
}

export interface CreateKeyRequest {
    name: string;
    deviceId: string;
    allowedIps: string;
    rateLimitPerMinute: number;
}

export interface TemplateRequest {
    code: string;
    name: string;
    content: string;
    description: string;
    enabled: boolean;
}

// ---------- Key ----------
export const listKeys = (): Promise<{ keys: APIKey[]; total: number }> =>
    apiClient.get('/openapi/keys');

export const createKey = (req: CreateKeyRequest): Promise<{ key: APIKey; secret: string; notice: string }> =>
    apiClient.post('/openapi/keys', req);

export const updateKey = (id: string, req: CreateKeyRequest & { enabled: boolean }) =>
    apiClient.put(`/openapi/keys/${id}`, req);

export const deleteKey = (id: string) =>
    apiClient.delete(`/openapi/keys/${id}`);

export const resetSecret = (id: string): Promise<{ secret: string; notice: string }> =>
    apiClient.post(`/openapi/keys/${id}/reset-secret`);

// ---------- 模板 ----------
export const listTemplates = (): Promise<{ templates: SMSTemplate[]; total: number }> =>
    apiClient.get('/openapi/templates');

export const createTemplate = (req: TemplateRequest) =>
    apiClient.post('/openapi/templates', req);

export const updateTemplate = (id: string, req: TemplateRequest) =>
    apiClient.put(`/openapi/templates/${id}`, req);

export const deleteTemplate = (id: string) =>
    apiClient.delete(`/openapi/templates/${id}`);

// ---------- 日志 ----------
export const listSendLogs = (limit = 50): Promise<{ logs: APISendLog[]; total: number }> =>
    apiClient.get(`/openapi/logs?limit=${limit}`);
