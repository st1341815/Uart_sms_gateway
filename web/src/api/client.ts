// Fetch API 客户端

const BASE_URL = '/api';

/** 未登录（401）时抛出，调用方可据此静默处理而不弹错误提示 */
export class UnauthorizedError extends Error {
    constructor(message = '未授权，请重新登录') {
        super(message);
        this.name = 'UnauthorizedError';
    }
}

interface RequestOptions extends RequestInit {
    params?: Record<string, unknown>;
}

class ApiClient {
    private readonly baseURL: string;

    constructor(baseURL: string) {
        this.baseURL = baseURL;
    }

    private buildURL(path: string, params?: Record<string, unknown>): string {
        const url = new URL(this.baseURL + path, window.location.origin);

        if (params) {
            Object.entries(params).forEach(([key, value]) => {
                if (value !== undefined && value !== null && value !== '') {
                    url.searchParams.append(key, String(value));
                }
            });
        }

        return url.toString();
    }

    private async request<T>(
        path: string,
        options: RequestOptions = {}
    ): Promise<T> {
        const {params, ...fetchOptions} = options;

        const url = this.buildURL(path, params);

        // 获取 token
        const token = localStorage.getItem('token');

        const headers: HeadersInit = {
            'Content-Type': 'application/json',
            ...(token && { Authorization: `Bearer ${token}` }),
            ...fetchOptions.headers,
        };

        let response: Response;
        try {
            response = await fetch(url, {
                ...fetchOptions,
                headers,
            });
        } catch (error) {
            // 网络层错误（断网、服务未启动等）
            console.error('API 网络请求失败:', error);
            throw error;
        }

        // 处理未授权
        if (response.status === 401) {
            // 清除 localStorage 中的 token
            localStorage.removeItem('token');
            localStorage.removeItem('username');

            // 跳转到登录页面
            if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
                window.location.href = '/login';
            }
            throw new UnauthorizedError();
        }

        // 处理错误响应
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || `HTTP ${response.status}: ${response.statusText}`);
        }

        // 204 / 空响应体
        if (response.status === 204) {
            return undefined as T;
        }

        // 解析 JSON 响应
        return (await response.json()) as T;
    }

    async get<T>(path: string, options?: RequestOptions): Promise<T> {
        return this.request<T>(path, {...options, method: 'GET'});
    }

    async post<T>(path: string, data?: unknown, options?: RequestOptions): Promise<T> {
        return this.request<T>(path, {
            ...options,
            method: 'POST',
            body: JSON.stringify(data),
        });
    }

    async put<T>(path: string, data?: unknown, options?: RequestOptions): Promise<T> {
        return this.request<T>(path, {
            ...options,
            method: 'PUT',
            body: JSON.stringify(data),
        });
    }

    async delete<T>(path: string, options?: RequestOptions): Promise<T> {
        return this.request<T>(path, {...options, method: 'DELETE'});
    }
}

const apiClient = new ApiClient(BASE_URL);

export default apiClient;
