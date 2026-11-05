import {useState} from 'react';
import {Calendar, Clock, Edit, MessageSquare, Phone, Plus, Play, Smartphone, Trash2, CheckCircle2, XCircle} from 'lucide-react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {toast} from 'sonner';
import {PageHeader} from '@/components/PageHeader';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {getDevicesList} from '../api/serial';
import {getApiErrorMessage} from '@/lib/api-error';
import {
    createScheduledTask,
    deleteScheduledTask,
    getScheduledTasks,
    type ScheduledTask,
    type LastRunStatus,
    triggerScheduledTask,
    updateScheduledTask,
} from '../api/scheduled_task';

interface TaskFormData {
    name: string;
    enabled: boolean;
    intervalDays: number;
    phoneNumber: string;
    content: string;
    /** 留空 = 使用默认设备（按 config 声明顺序取第一个） */
    deviceId: string;
}

export default function ScheduledTasksConfig() {
    const queryClient = useQueryClient();
    // 设备列表（用于「发送设备」下拉）
    const {data: devicesResp} = useQuery({
        queryKey: ['devices-for-scheduled'],
        queryFn: getDevicesList,
        staleTime: 30000,
    });
    const devices = devicesResp?.devices ?? [];

    const [dialogOpen, setDialogOpen] = useState(false);
    const [editingTask, setEditingTask] = useState<ScheduledTask | null>(null);
    const [formData, setFormData] = useState<TaskFormData>({
        name: '',
        enabled: false,
        intervalDays: 90,
        phoneNumber: '',
        content: '',
        deviceId: '',
    });

    // 获取状态显示信息
    const getStatusDisplay = (status?: LastRunStatus) => {
        switch (status) {
            case 'success':
                return {
                    icon: CheckCircle2,
                    text: '成功',
                    colorClass: 'text-ok',
                    bgClass: 'bg-ok/10',
                };
            case 'failed':
                return {
                    icon: XCircle,
                    text: '失败',
                    colorClass: 'text-err',
                    bgClass: 'bg-err/10',
                };
            case 'unknown':
            default:
                return {
                    icon: Clock,
                    text: '发送中',
                    colorClass: 'text-brand',
                    bgClass: 'bg-brand-soft',
                };
        }
    };

    // 获取定时任务列表
    const {data: tasks = [], isLoading} = useQuery({
        queryKey: ['scheduledTasks'],
        queryFn: getScheduledTasks,
    });

    // 创建任务 mutation
    const createMutation = useMutation({
        mutationFn: createScheduledTask,
        onSuccess: () => {
            queryClient.invalidateQueries({queryKey: ['scheduledTasks']});
            setDialogOpen(false);
            resetForm();
            toast.success('任务创建成功');
        },
        onError: (error: unknown) => {
            console.error('创建任务失败:', error);
            toast.error(getApiErrorMessage(error, '创建任务失败'));
        },
    });

    // 更新任务 mutation
    const updateMutation = useMutation({
        mutationFn: ({id, task}: { id: string; task: TaskFormData }) =>
            updateScheduledTask(id, task),
        onSuccess: () => {
            queryClient.invalidateQueries({queryKey: ['scheduledTasks']});
            setDialogOpen(false);
            setEditingTask(null);
            resetForm();
            toast.success('任务更新成功');
        },
        onError: (error: unknown) => {
            console.error('更新任务失败:', error);
            toast.error(getApiErrorMessage(error, '更新任务失败'));
        },
    });

    // 删除任务 mutation
    const deleteMutation = useMutation({
        mutationFn: deleteScheduledTask,
        onSuccess: () => {
            queryClient.invalidateQueries({queryKey: ['scheduledTasks']});
            toast.success('任务删除成功');
        },
        onError: (error: unknown) => {
            console.error('删除任务失败:', error);
            toast.error(getApiErrorMessage(error, '删除任务失败'));
        },
    });

    // 触发任务 mutation
    const triggerMutation = useMutation({
        mutationFn: triggerScheduledTask,
        onSuccess: () => {
            queryClient.invalidateQueries({queryKey: ['scheduledTasks']});
            toast.success('任务已触发执行');
        },
        onError: (error: unknown) => {
            console.error('触发任务失败:', error);
            toast.error(getApiErrorMessage(error, '触发任务失败'));
        },
    });

    // 重置表单
    const resetForm = () => {
        setFormData({
            name: '',
            enabled: false,
            intervalDays: 90,
            phoneNumber: '',
            content: '',
            deviceId: '',
        });
    };

    // 打开添加对话框
    const handleOpenAddDialog = () => {
        setEditingTask(null);
        resetForm();
        setDialogOpen(true);
    };

    // 打开编辑对话框
    const handleOpenEditDialog = (task: ScheduledTask) => {
        setEditingTask(task);
        setFormData({
            name: task.name,
            enabled: task.enabled,
            intervalDays: task.intervalDays,
            phoneNumber: task.phoneNumber,
            content: task.content,
            deviceId: task.deviceId || '',
        });
        setDialogOpen(true);
    };

    // 更新表单字段
    const updateFormField = (field: keyof TaskFormData, value: TaskFormData[keyof TaskFormData]) => {
        setFormData({
            ...formData,
            [field]: value,
        });
    };

    // 提交表单
    const handleSubmit = () => {
        // 验证必填字段
        if (!formData.name.trim()) {
            toast.warning('请输入任务名称');
            return;
        }
        if (!formData.intervalDays || formData.intervalDays <= 0) {
            toast.warning('请输入有效的执行间隔天数（必须大于0）');
            return;
        }
        if (!formData.phoneNumber.trim()) {
            toast.warning('请输入目标手机号');
            return;
        }
        if (!formData.deviceId) {
            toast.warning('请选择发送设备');
            return;
        }
        if (!formData.content.trim()) {
            toast.warning('请输入短信内容');
            return;
        }

        if (editingTask) {
            // 更新任务
            updateMutation.mutate({id: editingTask.id, task: formData});
        } else {
            // 创建任务
            createMutation.mutate(formData);
        }
    };

    // 删除任务
    const handleDeleteTask = (id: string) => {
        if (confirm('确定要删除这个任务吗？')) {
            deleteMutation.mutate(id);
        }
    };

    // 触发任务
    const handleTriggerTask = (id: string) => {
        if (confirm('确定要立即执行这个任务吗？')) {
            triggerMutation.mutate(id);
        }
    };

    if (isLoading) {
        return (
            <div className="flex justify-center items-center py-20">
                <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-brand"></div>
            </div>
        );
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-300">
            <PageHeader
                title="定时任务配置"
                description="配置定期发送短信的任务，例如每 90 天发送一次流量查询短信"
                action={<Button
                    onClick={handleOpenAddDialog}
                    className="h-8 gap-1.5 rounded-md bg-brand px-3 text-xs font-bold text-brand-fg transition-opacity hover:opacity-90"
                >
                    <Plus className="size-3.5"/>
                    新建任务
                </Button>}
            />

            {tasks.length === 0 ? (
                <div className="text-center py-20 bg-surface rounded-md border border-line">
                    <div className="size-16 bg-brand-soft rounded-full flex items-center justify-center mx-auto mb-4">
                        <Clock className="size-8 text-brand"/>
                    </div>
                    <p className="text-sm text-fg-3 mb-2 font-medium">暂无任务</p>
                    <p className="text-xs text-fg-3">点击"新建任务"开始配置定时短信发送</p>
                </div>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {tasks.map((task) => (
                        <Card key={task.id}
                              className="border-line transition-all duration-200 group relative overflow-hidden">
                            <CardHeader className="border-b border-line-soft bg-surface-2">
                                <div className="flex items-start justify-between">
                                    <div className="flex items-center space-x-2.5 flex-1 min-w-0">
                                        <div
                                            className={`p-2 rounded-md ${task.enabled ? 'bg-brand-soft text-brand' : 'bg-surface-2 text-fg-3'}`}>
                                            <Calendar size={18}/>
                                        </div>
                                        <div className="flex-1 min-w-0">
                                            <CardTitle className="text-sm font-semibold text-fg truncate">
                                                {task.name}
                                            </CardTitle>
                                            <div className="flex items-center mt-1">
                                                <span
                                                    className={`w-1.5 h-1.5 rounded-full mr-1.5 ${task.enabled ? 'bg-ok animate-pulse' : 'bg-surface-3'}`}></span>
                                                <span className="text-xs text-fg-3 font-medium">
                          {task.enabled ? '运行中' : '已暂停'}
                        </span>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            </CardHeader>

                            <CardContent className="">
                                <div className="space-y-2 mb-3">
                                    <div
                                        className="flex items-start space-x-2 bg-surface-2 p-2.5 rounded-md border border-line-soft">
                                        <Clock size={14} className="text-fg-3 mt-0.5 flex-shrink-0"/>
                                        <div className="flex-1 min-w-0">
                                            <span
                                                className="text-xs text-fg-3 font-medium block mb-0.5">执行间隔</span>
                                            <span
                                                className="num text-sm text-fg-2 font-semibold">每 {task.intervalDays} 天</span>
                                        </div>
                                    </div>

                                    <div
                                        className="flex items-start space-x-2 bg-surface-2 p-2.5 rounded-md border border-line-soft">
                                        <Phone size={14} className="text-fg-3 mt-0.5 flex-shrink-0"/>
                                        <div className="flex-1 min-w-0">
                                            <span
                                                className="text-xs text-fg-3 font-medium block mb-0.5">目标号码</span>
                                            <span
                                                className="num text-sm text-fg-2 font-semibold">{task.phoneNumber}</span>
                                        </div>
                                    </div>

                                    <div
                                        className="flex items-start space-x-2 bg-surface-2 p-2.5 rounded-md border border-line-soft">
                                        <MessageSquare size={14} className="text-fg-3 mt-0.5 flex-shrink-0"/>
                                        <div className="flex-1 min-w-0">
                                            <span
                                                className="text-xs text-fg-3 font-medium block mb-0.5">短信内容</span>
                                            <p className="text-sm leading-[1.5] text-fg-2 line-clamp-2 break-words">{task.content}</p>
                                        </div>
                                    </div>
                                </div>

                                {task.lastRunAt > 0 && (
                                    <div className="mb-3 pb-2.5 border-b border-line-soft space-y-2">
                                        <div className="flex items-center text-xs">
                                            <span className="text-fg-3">上次执行：</span>
                                            <span className="text-fg-2 ml-1.5 font-medium">
                        {new Date(task.lastRunAt).toLocaleString('zh-CN', {
                            month: 'short',
                            day: 'numeric',
                            hour: '2-digit',
                            minute: '2-digit'
                        })}
                      </span>
                                        </div>
                                        {task.lastRunStatus && (
                                            <div className="flex items-center gap-1.5">
                                                <span className="text-fg-3 text-xs">执行状态：</span>
                                                {(() => {
                                                    const statusInfo = getStatusDisplay(task.lastRunStatus);
                                                    const StatusIcon = statusInfo.icon;
                                                    return (
                                                        <div
                                                            className={`flex items-center gap-1 px-2 py-0.5 rounded-full ${statusInfo.bgClass}`}>
                                                            <StatusIcon className={`size-3 ${statusInfo.colorClass}`}/>
                                                            <span
                                                                className={`text-xs font-medium ${statusInfo.colorClass}`}>
                                                                {statusInfo.text}
                                                            </span>
                                                        </div>
                                                    );
                                                })()}
                                            </div>
                                        )}
                                    </div>
                                )}

                                <div className="flex space-x-2 pt-1">
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={() => handleTriggerTask(task.id)}
                                        disabled={triggerMutation.isPending}
                                        className="flex-1 hover:border-ok/40 hover:bg-ok/10 hover:text-ok transition-colors text-xs font-medium"
                                    >
                                        <Play className="size-3.5 mr-1.5"/>
                                        触发
                                    </Button>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={() => handleOpenEditDialog(task)}
                                        className="flex-1 hover:bg-surface-2 hover:border-line transition-colors text-xs font-medium"
                                    >
                                        <Edit className="size-3.5 mr-1.5"/>
                                        编辑
                                    </Button>
                                    <Button
                                        variant="destructive"
                                        size="sm"
                                        onClick={() => handleDeleteTask(task.id)}
                                        disabled={deleteMutation.isPending}
                                        className="px-3 hover:bg-err/90 transition-colors text-xs font-medium"
                                    >
                                        <Trash2 className="size-3.5"/>
                                    </Button>
                                </div>
                            </CardContent>
                        </Card>
                    ))}
                </div>
            )}

            {/* 添加/编辑任务对话框 */}
            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                <DialogContent className="sm:max-w-[500px]">
                    <DialogHeader>
                        <DialogTitle className="font-semibold text-fg">
                            {editingTask ? '编辑任务' : '新建任务'}
                        </DialogTitle>
                        <DialogDescription className="text-xs text-fg-3">
                            {editingTask ? '修改定时任务的配置信息' : '创建新的定时短信任务，系统将按照设定的间隔自动发送'}
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-5 py-2">
                        {/* 任务名称 */}
                        <div>
                            <label className="block text-xs font-semibold text-fg-2 mb-2 uppercase tracking-[0.1em]">
                                任务名称 <span className="text-err">*</span>
                            </label>
                            <Input
                                value={formData.name}
                                onChange={(e) => updateFormField('name', e.target.value)}
                                placeholder="例如：90天流量查询"
                                className="bg-surface-2 border-line focus:bg-surface focus:border-brand focus:ring-1 focus:ring-brand transition-all"
                            />
                        </div>

                        {/* 启用状态 */}
                        <div className="bg-surface-2 border border-line rounded-md p-3.5 flex items-center gap-3">
                            <input
                                type="checkbox"
                                id="enabled"
                                checked={formData.enabled}
                                onChange={(e) => updateFormField('enabled', e.target.checked)}
                                className="rounded border-line text-brand focus:ring-brand size-4 cursor-pointer"
                            />
                            <label htmlFor="enabled"
                                   className="text-sm font-medium text-fg-2 cursor-pointer flex-1">
                                启用此任务
                            </label>
                            <div
                                className={`w-2 h-2 rounded-full ${formData.enabled ? 'bg-ok' : 'bg-surface-3'}`}></div>
                        </div>

                        <div className="grid grid-cols-2 gap-4">
                            {/* 执行间隔天数 */}
                            <div className="col-span-2 sm:col-span-1">
                                <label
                                    className="block text-xs font-semibold text-fg-2 mb-2 uppercase tracking-[0.1em] flex items-center gap-1.5">
                                    <Clock size={12} className="text-fg-3"/>
                                    执行间隔 <span className="text-err">*</span>
                                </label>
                                <div className="relative">
                                    <Input
                                        type="number"
                                        min="1"
                                        value={formData.intervalDays}
                                        onChange={(e) => updateFormField('intervalDays', parseInt(e.target.value) || 0)}
                                        placeholder="90"
                                        className="bg-surface-2 border-line focus:bg-surface focus:border-brand focus:ring-1 focus:ring-brand transition-all pr-12"
                                    />
                                    <span
                                        className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-fg-3 font-medium">天</span>
                                </div>
                                <p className="text-xs text-fg-3 mt-1.5">
                                    任务执行的时间间隔
                                </p>
                            </div>

                            {/* 目标手机号 */}
                            <div className="col-span-2 sm:col-span-1">
                                <label
                                    className="block text-xs font-semibold text-fg-2 mb-2 uppercase tracking-[0.1em] flex items-center gap-1.5">
                                    <Phone size={12} className="text-fg-3"/>
                                    目标号码 <span className="text-err">*</span>
                                </label>
                                <Input
                                    value={formData.phoneNumber}
                                    onChange={(e) => updateFormField('phoneNumber', e.target.value)}
                                    placeholder="10086"
                                    className="bg-surface-2 border-line focus:bg-surface focus:border-brand focus:ring-1 focus:ring-brand transition-all font-mono"
                                />
                                <p className="text-xs text-fg-3 mt-1.5">
                                    接收短信的手机号码
                                </p>
                            </div>
                        </div>

                        {/* 发送设备 */}
                        <div>
                            <label
                                className="block text-xs font-semibold text-fg-2 mb-2 uppercase tracking-[0.1em] flex items-center gap-1.5">
                                <Smartphone size={12} className="text-fg-3"/>
                                发送设备 <span className="text-err">*</span>
                            </label>
                            <select
                                value={formData.deviceId}
                                onChange={(e) => updateFormField('deviceId', e.target.value)}
                                className="h-9 w-full rounded-md border border-line bg-surface-2 px-3 text-sm outline-none transition-colors focus:border-brand focus:bg-surface"
                            >
                                <option value="">请选择设备（必选）</option>
                                {devices.map((d) => (
                                    <option key={d.id} value={d.id}>
                                        {d.name || d.id}（{d.id}{d.description ? ' · ' + d.description : ''}）
                                    </option>
                                ))}
                            </select>
                            <p className="mt-1.5 text-xs leading-4 text-warn">
                                必须选择：短信只会从所选卡发出，避免误用境外漫游卡产生国际资费。
                            </p>
                        </div>

                        {/* 短信内容 */}
                        <div>
                            <label
                                className="block text-xs font-semibold text-fg-2 mb-2 uppercase tracking-[0.1em] flex items-center gap-1.5">
                                <MessageSquare size={12} className="text-fg-3"/>
                                短信内容 <span className="text-err">*</span>
                            </label>
                            <textarea
                                value={formData.content}
                                onChange={(e) => updateFormField('content', e.target.value)}
                                placeholder="例如：查询流量、CXLL 等"
                                rows={3}
                                className="w-full bg-surface-2 border border-line rounded-md px-3 py-2 text-sm focus:bg-surface focus:border-brand focus:ring-1 focus:ring-brand transition-all outline-none resize-none"
                            />
                            <p className="text-xs text-err mt-1.5">
                                将要发送的短信内容，不支持 Emoji 等特殊字符。
                            </p>
                        </div>
                    </div>

                    <DialogFooter className="bg-surface-2 -mx-6 -mb-6 px-6 py-4 rounded-b-lg border-t border-line-soft">
                        <Button
                            variant="outline"
                            onClick={() => {
                                setDialogOpen(false);
                                setEditingTask(null);
                                resetForm();
                            }}
                            disabled={createMutation.isPending || updateMutation.isPending}
                            className="hover:bg-surface transition-colors"
                        >
                            取消
                        </Button>
                        <Button
                            onClick={handleSubmit}
                            disabled={createMutation.isPending || updateMutation.isPending}
                            className="bg-brand hover:bg-brand transition-colors min-w-[100px]"
                        >
                            {createMutation.isPending || updateMutation.isPending
                                ? '提交中...'
                                : editingTask
                                    ? '更新任务'
                                    : '创建任务'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
