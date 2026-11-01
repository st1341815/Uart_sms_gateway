import {useState, useEffect} from 'react';
import {Activity, RotateCcw, Send, Signal, Wifi, Smartphone} from 'lucide-react';
import {toast} from 'sonner';
import {useMutation, useQuery} from '@tanstack/react-query';
import * as serialApi from '../api/serial';
import {Input} from '@/components/ui/input';
import {Textarea} from '@/components/ui/textarea';
import {Button} from '@/components/ui/button';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {DeviceSelector} from '@/components/DeviceSelector';
import type {DeviceStatusSummary, DeviceStatus} from '@/api/types';
import {formatUptime} from "@/utils/utils.ts";

export default function SerialControl() {
    const [to, setTo] = useState('');
    const [content, setContent] = useState('');
    const [selectedDeviceId, setSelectedDeviceId] = useState<string>('');

    // 获取所有设备列表
    const {data: devicesResponse, isFetching: devicesLoading} = useQuery({
        queryKey: ['devices'],
        queryFn: async () => {
            const res = await serialApi.getAllDevicesStatus();
            return res;
        },
        refetchInterval: 10000, // 每 10 秒自动刷新
    });

    // 设备列表
    const devices = devicesResponse?.devices || [];

    // 默认选择第一个在线设备
    useEffect(() => {
        if (devices.length > 0 && !selectedDeviceId) {
            const firstOnline = devices.find(d => d.connected);
            setSelectedDeviceId(firstOnline?.id || devices[0]?.id || '');
        }
    }, [devices, selectedDeviceId]);

    // 当前选中的设备
    const selectedDevice = devices.find(d => d.id === selectedDeviceId);

    // 获取单个设备详细状态
    const {data: deviceDetail, refetch: refetchDetail} = useQuery({
        queryKey: ['deviceDetail', selectedDeviceId],
        queryFn: async () => {
            if (!selectedDeviceId) return null;
            const res = await serialApi.getDeviceStatus(selectedDeviceId);
            return res as DeviceStatusSummary;
        },
        enabled: !!selectedDeviceId,
        refetchInterval: 10000,
    });

    // 发送短信 Mutation（支持指定设备）
    const sendSMSMutation = useMutation({
        mutationFn: (data: { to: string; content: string; device_id?: string }) => 
            serialApi.sendSMS(data),
        onSuccess: () => {
            toast.success(`短信下发成功（${selectedDevice?.name || '默认设备'}），等待确认...`);
            setTo('');
            setContent('');
        },
        onError: (error) => {
            console.error('发送失败:', error);
            toast.error('发送失败');
        },
    });

    // 设置飞行模式 Mutation
    const setFlymodeMutation = useMutation({
        mutationFn: (enabled: boolean) => serialApi.setFlymode(enabled, selectedDeviceId),
        onSuccess: () => {
            toast.success(`设置成功（${selectedDevice?.name || '默认设备'}）`);
            refetchDetail();
        },
        onError: (error) => {
            console.error('操作失败:', error);
            toast.error('操作失败');
        },
    });

    // 重启模块 Mutation
    const rebootMcuMutation = useMutation({
        mutationFn: () => serialApi.rebootMcu(selectedDeviceId),
        onSuccess: () => {
            toast.success(`模块重启命令已发送（${selectedDevice?.name || '默认设备'}）`);
            refetchDetail();
        },
        onError: (error) => {
            console.error('操作失败:', error);
            toast.error('操作失败');
        },
    });

    const handleSendSMS = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!to || !content) {
            toast.warning('请输入手机号和短信内容');
            return;
        }
        if (!selectedDeviceId) {
            toast.warning('请选择发送设备');
            return;
        }
        sendSMSMutation.mutate({to, content, device_id: selectedDeviceId});
    };

    // 从设备状态中获取移动网络信息
    const mobile = selectedDevice;

    return (
        <div className="flex flex-col overflow-hidden">
            {/* 顶部标题 */}
            <div className="mb-4">
                <h1 className="text-2xl font-bold text-gray-900">串口控制</h1>
                <p className="text-sm text-gray-500 mt-1">多设备短信转发管理</p>
            </div>

            {/* 设备选择器 */}
            <Card className="mb-4">
                <CardHeader className="pb-3">
                    <CardTitle className="flex items-center gap-2 text-base">
                        <Smartphone className="w-4 h-4 text-blue-600"/>
                        选择设备
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    <DeviceSelector
                        devices={devices}
                        selectedDeviceId={selectedDeviceId}
                        onSelect={setSelectedDeviceId}
                        disabled={devicesLoading}
                    />
                    <div className="mt-2 flex gap-2">
                        {devices.map(d => (
                            <div 
                                key={d.id}
                                className={`px-2 py-1 rounded text-xs cursor-pointer transition-colors ${
                                    d.id === selectedDeviceId 
                                        ? 'bg-blue-100 text-blue-700 border border-blue-300' 
                                        : 'bg-gray-50 text-gray-500 hover:bg-gray-100'
                                }`}
                                onClick={() => setSelectedDeviceId(d.id)}
                            >
                                <div className="flex items-center gap-1">
                                    <div className={`w-1.5 h-1.5 rounded-full ${
                                        d.connected ? 'bg-green-600' : 'bg-red-600'
                                    }`} />
                                    {d.name}
                                </div>
                            </div>
                        ))}
                    </div>
                </CardContent>
            </Card>

            {/* 主内容区 - 三列布局 */}
            <div className="flex-1 grid grid-cols-1 lg:grid-cols-3 gap-4 min-h-0">
                {/* 左侧：移动网络信息 */}
                <Card className="flex flex-col min-h-0">
                    <CardHeader className="pb-3">
                        <CardTitle className="flex items-center gap-2 text-base">
                            <Signal className="w-4 h-4 text-blue-600"/>
                            移动网络信息
                            {selectedDevice && (
                                <span className="text-xs text-gray-400 font-mono">
                                    ({selectedDevice.name})
                                </span>
                            )}
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="flex-1 overflow-y-auto">
                        {mobile ? (
                            <div className="space-y-3">
                                <div className="flex justify-between items-center pb-2 border-b">
                                    <span className="text-xs text-gray-500">连接状态</span>
                                    <span className="text-sm font-medium">
                                        {mobile.connected ? (
                                            <span className="text-green-600 flex items-center gap-1">
                                                <div className="w-1.5 h-1.5 rounded-full bg-green-600"></div>
                                                已连接
                                            </span>
                                        ) : (
                                            <span className="text-red-600 flex items-center gap-1">
                                                <div className="w-1.5 h-1.5 rounded-full bg-red-600"></div>
                                                未连接
                                            </span>
                                        )}
                                    </span>
                                </div>
                                <div className="flex justify-between items-center pb-2 border-b">
                                    <span className="text-xs text-gray-500">运营商</span>
                                    <span className="text-sm font-medium">
                                        {mobile.operator || '-'}
                                    </span>
                                </div>
                                <div className="flex justify-between items-center pb-2 border-b">
                                    <span className="text-xs text-gray-500">信号强度</span>
                                    <span className="text-sm font-medium">
                                        {mobile.signal || '-'}
                                    </span>
                                </div>
                                <div className="flex justify-between items-center pb-2 border-b">
                                    <span className="text-xs text-gray-500">漫游状态</span>
                                    <span className="text-sm font-medium">
                                        {mobile.isRoaming ? (
                                            <span className="text-yellow-600">漫游中</span>
                                        ) : (
                                            <span className="text-green-600">本地</span>
                                        )}
                                    </span>
                                </div>
                                <div className="pt-1">
                                    <div className="text-xs text-gray-500 mb-1">ICCID</div>
                                    <div className="font-mono text-xs bg-gray-50 p-1.5 rounded break-all">
                                        {mobile.iccid || '-'}
                                    </div>
                                </div>
                                <div className="pt-1">
                                    <div className="text-xs text-gray-500 mb-1">IMSI</div>
                                    <div className="font-mono text-xs bg-gray-50 p-1.5 rounded break-all">
                                        {mobile.imsi || '-'}
                                    </div>
                                </div>
                                {mobile.phoneNumber && (
                                    <div className="pt-1">
                                        <div className="text-xs text-gray-500 mb-1">手机号</div>
                                        <div className="font-mono text-xs bg-gray-50 p-1.5 rounded break-all">
                                            {mobile.phoneNumber}
                                        </div>
                                    </div>
                                )}
                            </div>
                        ) : (
                            <div className="flex flex-col items-center justify-center h-full text-gray-400">
                                <Wifi className="w-12 h-12 mb-2 opacity-30 animate-pulse"/>
                                <p className="text-sm">加载中...</p>
                            </div>
                        )}
                    </CardContent>
                </Card>

                {/* 中间：发送短信 */}
                <Card className="flex flex-col min-h-0">
                    <CardHeader className="pb-3">
                        <CardTitle className="flex items-center gap-2 text-base">
                            <Send className="w-4 h-4 text-green-600"/>
                            发送短信
                            {selectedDevice && (
                                <span className="text-xs text-gray-400">
                                    → {selectedDevice.name}
                                </span>
                            )}
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="flex-1 flex flex-col">
                        <form onSubmit={handleSendSMS} className="flex flex-col h-full space-y-3">
                            <div>
                                <label className="block text-xs font-medium text-gray-700 mb-1.5">
                                    目标手机号
                                </label>
                                <Input
                                    type="tel"
                                    value={to}
                                    onChange={(e) => setTo(e.target.value)}
                                    placeholder="请输入手机号"
                                    className="h-9"
                                    required
                                />
                            </div>
                            <div className="flex-1 flex flex-col min-h-0">
                                <label className="block text-xs font-medium text-gray-700 mb-1.5">
                                    短信内容
                                </label>
                                <Textarea
                                    value={content}
                                    onChange={(e) => setContent(e.target.value)}
                                    placeholder="请输入短信内容"
                                    className="flex-1 resize-none"
                                    required
                                />
                            </div>
                            <Button
                                type="submit"
                                disabled={sendSMSMutation.isPending || !selectedDeviceId}
                                className="w-full bg-green-600 hover:bg-green-700 h-9"
                            >
                                <Send className="w-3.5 h-3.5 mr-2"/>
                                {sendSMSMutation.isPending ? '发送中...' : 
                                    selectedDevice ? `通过 ${selectedDevice.name} 发送` : '发送短信'}
                            </Button>
                        </form>
                    </CardContent>
                </Card>

                {/* 右侧：设备状态 + 控制 */}
                <div className="flex flex-col gap-4 min-h-0">
                    {/* 设备状态 */}
                    {selectedDevice && (
                        <Card className="flex-1 flex flex-col min-h-0 gap-2">
                            <CardHeader className="pb-3">
                                <CardTitle className="flex items-center gap-2 text-base">
                                    <Activity className="w-4 h-4 text-purple-600"/>
                                    设备状态
                                    <span className="text-xs text-gray-400 font-mono">
                                        ({selectedDevice.name})
                                    </span>
                                </CardTitle>
                            </CardHeader>
                            <CardContent className="flex-1 overflow-y-auto">
                                <div className="space-y-2">
                                    <div className="flex justify-between items-center pb-2 border-b">
                                        <span className="text-xs text-gray-500">串口连接</span>
                                        <span className="text-sm font-medium">
                                            {selectedDevice.connected ? (
                                                <span className="text-green-600 flex items-center gap-1">
                                                    <div className="w-1.5 h-1.5 rounded-full bg-green-600"></div>
                                                    已连接
                                                </span>
                                            ) : (
                                                <span className="text-red-600 flex items-center gap-1">
                                                    <div className="w-1.5 h-1.5 rounded-full bg-red-600"></div>
                                                    未连接
                                                </span>
                                            )}
                                        </span>
                                    </div>
                                    {selectedDevice.port && (
                                        <div className="flex justify-between items-center pb-2 border-b">
                                            <span className="text-xs text-gray-500">串口名称</span>
                                            <span className="text-sm font-medium font-mono">{selectedDevice.port}</span>
                                        </div>
                                    )}
                                    <div className="flex justify-between items-center pb-2 border-b">
                                        <span className="text-xs text-gray-500">飞行模式</span>
                                        <span className="text-sm font-medium">
                                            {selectedDevice.flymode ? (
                                                <span className="text-orange-600">已启用</span>
                                            ) : (
                                                <span className="text-green-600">已禁用</span>
                                            )}
                                        </span>
                                    </div>
                                </div>
                            </CardContent>
                        </Card>
                    )}

                    {/* 设备控制 */}
                    <Card className={'gap-2'}>
                        <CardHeader className="pb-3">
                            <CardTitle className="flex items-center gap-2 text-base">
                                <RotateCcw className="w-4 h-4 text-orange-600"/>
                                设备控制
                                {selectedDevice && (
                                    <span className="text-xs text-gray-400">
                                        ({selectedDevice.name})
                                    </span>
                                )}
                            </CardTitle>
                        </CardHeader>
                        <CardContent>
                            <div className="space-y-3">
                                <p className="text-xs text-gray-600">
                                    飞行模式状态：{selectedDevice?.flymode ? (
                                        <span className="text-orange-600 font-medium">已启用</span>
                                    ) : (
                                        <span className="text-green-600 font-medium">已禁用</span>
                                    )}
                                </p>
                                <div className="flex gap-2">
                                    <Button
                                        onClick={() => setFlymodeMutation.mutate(true)}
                                        disabled={setFlymodeMutation.isPending || !selectedDeviceId}
                                        variant="outline"
                                        className="flex-1 border-orange-300 text-orange-700 hover:bg-orange-50 h-9 cursor-pointer"
                                    >
                                        开启飞行模式
                                    </Button>
                                    <Button
                                        onClick={() => setFlymodeMutation.mutate(false)}
                                        disabled={setFlymodeMutation.isPending || !selectedDeviceId}
                                        variant="outline"
                                        className="flex-1 border-green-300 text-green-700 hover:bg-green-50 h-9 cursor-pointer"
                                    >
                                        关闭飞行模式
                                    </Button>
                                </div>
                                <div className="border-t pt-2">
                                    <Button
                                        onClick={() => rebootMcuMutation.mutate()}
                                        disabled={rebootMcuMutation.isPending || !selectedDeviceId}
                                        variant="outline"
                                        className="w-full border-orange-300 text-orange-700 hover:bg-orange-50 h-9"
                                    >
                                        <RotateCcw className="w-3.5 h-3.5 mr-2"/>
                                        重启模块
                                    </Button>
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                </div>
            </div>
        </div>
    );
}