// 统一的 API 错误信息提取
//
// 后端（Echo）出错时返回的是 JSON：{ "error": "..." } 或 { "message": "..." }。
// 但 apiClient 在 !response.ok 时抛的是 `new Error(响应文本)`，
// 所以错误对象上并没有 axios 风格的 response.data —— 这里兼容两种情况：
//   1. 上游若改成带 response 的结构（axios 风格），优先取 response.data.error
//   2. 目前实际形态：Error.message 是一段可能是 JSON 的字符串，尝试解析
// 目的是把 any 干掉，同时顺手修掉「错误提示显示一整段 JSON」的体验问题。

interface ApiErrorLike {
    response?: { data?: { error?: string; message?: string } };
    message?: string;
}

export function getApiErrorMessage(error: unknown, fallback: string): string {
    if (!error) return fallback;

    if (typeof error === 'string') return error || fallback;

    const e = error as ApiErrorLike;

    // axios 风格
    const fromResponse = e.response?.data?.error || e.response?.data?.message;
    if (fromResponse) return fromResponse;

    const msg = typeof e.message === 'string' ? e.message.trim() : '';
    if (!msg) return fallback;

    // apiClient 抛出的就是响应正文，可能是 {"error":"..."}
    if (msg.startsWith('{')) {
        try {
            const parsed = JSON.parse(msg) as { error?: string; message?: string };
            const inner = parsed.error || parsed.message;
            if (inner) return inner;
        } catch {
            // 不是 JSON（比如 HTML 错误页），落到下面的原样返回
        }
    }

    return msg;
}
