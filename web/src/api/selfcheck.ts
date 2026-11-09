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

// 单口试探记录（失败时这是唯一能说清「卡到底怎么了」的东西）
export interface ProbeAttempt {
    if_key: string;
    by_path: string;
    node: string;
    in_container: boolean;
    opened: boolean;
    got_status: boolean;
    error?: string;
}

// 探测结果（L1）
//
// success=false 是**正常业务结论**（卡没上电/没插好/固件没跑），
// 接口仍返回 200；只有「端口不存在 / 已配置拒绝 / 冷却 / 并发」才是 4xx。
export interface ProbeResult {
    usb_port: string;
    success: boolean;
    message: string;
    next_step?: string;
    tried: ProbeAttempt[];
    at_port?: string;
    node?: string;
    if_key?: string;
    at_confirmed: boolean;
    operator?: string;
    number?: string;
    iccid?: string;
    imsi?: string;
    sim_ready: boolean;
    is_registered: boolean;
    is_roaming: boolean;
    csq: number;
    rsrp: number;
    signal_desc?: string;
    lua_version?: string;
    /** 建议的设备显示名（运营商 + 号码） */
    name?: string;
    /** 带实测信息的 config.yaml Devices 片段 */
    config_snippet?: string;
    probed_at: string;
}

// 探测某个 USB 端口的 AT 口
//
// 会**真的打开串口**发一次 get_status，只允许对未被配置引用的端口调用；
// 同端口 30 秒冷却、全局串行（一次只探一个）。
export const probeModule = (usbPort: string): Promise<ProbeResult> => {
    return apiClient.post<ProbeResult>("/serial/probe", {usb_port: usbPort});
};
