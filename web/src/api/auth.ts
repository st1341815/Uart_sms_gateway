import apiClient from './client';

export interface LoginRequest {
    username: string;
    password: string;
}

export interface LoginResponse {
    token: string;
    username: string;
    expiresAt: number;
}

// 认证配置
export interface AuthConfig {
    oidcEnabled: boolean;
    githubEnabled: boolean;
    passwordEnabled: boolean;
}

// OIDC 认证 URL
export interface OIDCAuthURL {
    authUrl: string;
    state: string;
}

export const login = (request: LoginRequest): Promise<LoginResponse> => {
    return apiClient.post('/login', request);
};

// 获取认证配置
export const getAuthConfig = (): Promise<AuthConfig> => {
    return apiClient.get('/auth/config');
};

// 获取 OIDC 认证 URL
export const getOIDCAuthURL = (): Promise<OIDCAuthURL> => {
    return apiClient.get('/auth/oidc/url');
};

// OIDC 登录回调
export const oidcLogin = (code: string, state: string): Promise<LoginResponse> => {
    return apiClient.post('/auth/oidc/callback', {code, state});
};
// ===== 账号安全 =====

export interface MeResponse {
    username: string;
}

export interface ChangePasswordRequest {
    old_password: string;
    new_password: string;
}

export interface ChangePasswordResponse {
    message: string;
    require_relogin?: boolean;
}

// 当前登录用户
export const getMe = (): Promise<MeResponse> => {
    return apiClient.get('/auth/me');
};

// 修改密码
export const changePassword = (req: ChangePasswordRequest): Promise<ChangePasswordResponse> => {
    return apiClient.post('/auth/password', req);
};

// ===== 短信重置密码 =====

// 安全手机号（只回显脱敏值，后端不返回完整号码）
export interface ResetPhoneResponse {
    phone_set: boolean;
    masked_phone?: string;
}

export interface SetResetPhoneResponse {
    message: string;
    masked_phone: string;
}

export interface ResetBySMSRequest {
    device_id: string;
    // 留空则使用已保存的安全手机号
    phone?: string;
}

export interface ResetBySMSResponse {
    message: string;
    masked_phone: string;
    device_id: string;
    device_name: string;
    sent_at: number;
    require_relogin?: boolean;
}

// 读取已保存的安全手机号
export const getResetPhone = (): Promise<ResetPhoneResponse> => {
    return apiClient.get('/auth/password-reset/phone');
};

// 保存安全手机号
export const setResetPhone = (phone: string): Promise<SetResetPhoneResponse> => {
    return apiClient.put('/auth/password-reset/phone', {phone});
};

// 生成新密码并通过短信下发（不校验旧密码）
export const resetPasswordBySMS = (req: ResetBySMSRequest): Promise<ResetBySMSResponse> => {
    return apiClient.post('/auth/password-reset/sms', req);
};
