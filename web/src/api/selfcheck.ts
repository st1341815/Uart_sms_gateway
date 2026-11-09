import apiClient from "./client";

// 设备自检（L0 一致性体检）
//
// 后端所有字段都是蛇形命名（与 Go 的 json tag 一致），这里不做驼峰转换，
// 保持和 App 其它接口一致：直接用蛇形，避免「驼峰化之后忘记映射」的老坑。

export interface SelfCheckItem {
    /** ok | info | warn | error */
    level: 'ok' | 'info' | 'warn' | 'error';
    code: string;
    /** 给人看的结论（中文） */
    message: string;
    /** 可执行的下一步 */
    next_step?: string;
    device_id?: string;
    usb_port?: string;
}

export interface SelfCheckDevice {
    id: string;
    name: string;
    port: string;
    enabled: boolean;
    usb_port?: string;
    node?: string;
    /** by-path 软链在宿主机上存在 */
    on_host: boolean;
    /** 节点在容器 /dev 里存在 */
    in_container: boolean;
    /** ok | not_mapped | missing_on_host | node_only | disabled | auto | invalid */
    status: string;
    status_text: string;
}

export interface SelfCheckInterface {
    by_path: string;
    if_key: string;
    node: string;
    in_container: boolean;
}

export interface SelfCheckModule {
    usb_port: string;
    device?: string;
    id_vendor?: string;
    id_product?: string;
    interfaces: SelfCheckInterface[];
    candidate_port?: string;
    candidate_node?: string;
    at_confirmed: boolean;
    in_container: boolean;
    configured: boolean;
    device_ids?: string[];
    /** 可直接贴进 docker-compose.yml */
    compose_snippet?: string;
    /** 可直接贴进 config.yaml 的 Serial.Devices */
    config_snippet?: string;
}

export interface SelfCheckReport {
    generated_at: string;
    by_path_available: boolean;
    sys_available: boolean;
    devices: SelfCheckDevice[];
    modules: SelfCheckModule[];
    items: SelfCheckItem[];
    summary: {
        ok: number;
        warn: number;
        error: number;
        info: number;
    };
    /** 未被任何配置引用的模组数量 = 新卡数量 */
    unconfigured_count: number;
}

// 执行一次自检（只读，不会打开串口）
export const getSelfCheck = (): Promise<SelfCheckReport> => {
    return apiClient.get<SelfCheckReport>("/serial/selfcheck");
};
