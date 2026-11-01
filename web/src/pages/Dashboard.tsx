import {useEffect, useState} from 'react';
import {Globe, MessageSquare, Signal, TrendingUp, Smartphone} from 'lucide-react';
import {getStats} from '../api/messages';
import type {Stats, DeviceStatusSummary} from '../api/types';
import {StatCard} from "@/components/StatsCard.tsx";
import {useQuery} from "@tanstack/react-query";
import {getAllDevicesStatus} from "@/api/serial.ts";
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';

export default function Dashboard() {
    const [stats, setStats] = useState<Stats | null>(null);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        loadStats();
        const interval = setInterval(loadStats, 30000); // 每30秒刷新
        return () => clearInterval(interval);
    }, []);

    const loadStats = async () => {
        try {
            const data = await getStats();
            setStats(data);
        } catch (error) {
            console.error('获取统计信息失败:', error);
        } finally {
            setLoading(false);
        }
    };

    // 获取所有设备状态 - 每 10 秒自动刷新
    const {data: devicesResponse} = useQuery({
        queryKey: ['allDevicesStatus'],
        queryFn: async () => {
            const res = await getAllDevicesStatus();
            return res;
        },
        refetchInterval: 10000,
    });

    // 设备列表
    const devices = devicesResponse?.devices || [];
    
    // 在线设备数量
    const onlineDevices = devices.filter(d => d.connected).length;

    // 获取第一个设备的信号（用于显示）
    const firstDevice = devices[0];
    
    // 计算信号强度百分比
    const getSignalPercentage = (device?: DeviceStatusSummary) => {
        if (!device?.signal) return 0;
        // signal_level: 0-31
        return Math.round((device.signal / 31) * 100);
    };

    // 获取信号描述
    const getSignalDescription = (device?: DeviceStatusSummary) => {
        if (!device?.signal) return 'N/A';
        const signal = device.signal;
        if (signal >= 20) return '优秀';
        if (signal >= 15) return '良好';
        if (signal >= 10) return '一般';
        return '较差';
    };

    if (loading) {
        return (
            <div className="flex justify-center items-center h-64">
                <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
            </div>
        );
    }

    return (
        <div>
            <h1 className="text-2xl font-bold text-gray-900 mb-2">统计面板</h1>
            <p className="text-sm text-gray-500 mb-6">
                多设备短信转发系统 • {onlineDevices}/{devices.length} 设备在线
            </p>

            {/* 设备状态卡片 */}
            {devices.length > 0 && (
                <div className="mb-6">
                    <Card>
                        <CardHeader className="pb-3">
                            <CardTitle className="flex items-center gap-2 text-base">
                                <Smartphone className="w-4 h-4 text-blue-600"/>
                                设备状态概览
                            </CardTitle>
                        </CardHeader>
                        <CardContent>
                            <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-4 gap-4">
                                {devices.map(device => (
                                    <div 
                                        key={device.id}
                                        className={`p-4 rounded-lg border ${
                                            device.connected 
                                                ? 'bg-green-50 border-green-200' 
                                                : 'bg-gray-50 border-gray-200'
                                        }`}
                                    >
                                        <div className="flex items-center gap-2 mb-2">
                                            <div className={`w-2 h-2 rounded-full ${
                                                device.connected ? 'bg-green-600' : 'bg-red-600'
                                            }`} />
                                            <span className="font-medium text-sm">{device.name}</span>
                                        </div>
                                        <div className="text-xs text-gray-500 space-y-1">
                                            <div className="flex justify-between">
                                                <span>运营商</span>
                                                <span className="font-mono">{device.operator || '-'}</span>
                                            </div>
                                            <div className="flex justify-between">
                                                <span>信号</span>
                                                <span>{device.signal || '-'}</span>
                                            </div>
                                            <div className="flex justify-between">
                                                <span>漫游</span>
                                                <span className={device.isRoaming ? 'text-yellow-600' : 'text-green-600'}>
                                                    {device.isRoaming ? '是' : '否'}
                                                </span>
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </CardContent>
                    </Card>
                </div>
            )}

            {/* 统计卡片 */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
                <StatCard 
                    label="设备状态" 
                    value={onlineDevices} 
                    unit={`/ ${devices.length}`} 
                    icon={Smartphone}
                    colorClass="bg-blue-100 text-blue-600"
                    subValue={`${onlineDevices} 个在线`}
                />
                <StatCard 
                    label="信号强度" 
                    value={getSignalPercentage(firstDevice)} 
                    unit="%" 
                    icon={Signal}
                    colorClass="bg-green-100 text-green-600"
                    subValue={`${getSignalDescription(firstDevice)} • ${firstDevice?.operator || '-'}`}
                />
                <StatCard 
                    label="总短信数" 
                    value={stats?.totalCount || 0} 
                    icon={MessageSquare}
                    colorClass="bg-green-100 text-green-600"
                    subValue={`今日: ${stats?.todayCount || 0}`}
                />
                <StatCard 
                    label="今日短信" 
                    value={stats?.todayCount || 0} 
                    icon={TrendingUp}
                    colorClass="bg-purple-100 text-purple-600"
                    subValue={`接收: ${stats?.incomingCount || 0} • 发送: ${stats?.outgoingCount || 0}`}
                />
            </div>

            {/* 系统信息 */}
            <div className="mt-8 bg-white rounded-lg shadow-md p-6">
                <h2 className="text-lg font-semibold text-gray-900 mb-4">系统信息</h2>
                <div className="space-y-2 text-sm text-gray-600">
                    <p>• 支持多设备短信转发管理</p>
                    <p>• 自动接收短信并发送通知到配置的渠道</p>
                    <p>• 自动接收来电并发送通知</p>
                    <p>• 支持定时发送短信（可指定设备）</p>
                    <p>• 支持手动发送短信和串口控制</p>
                    <p>• 短信记录自动保存到数据库</p>
                    <p className="pt-2 text-xs text-gray-400">
                        • 当前运行 {devices.length} 个短信转发设备
                    </p>
                </div>
            </div>
        </div>
    );
}