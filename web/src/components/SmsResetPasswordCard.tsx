import {useEffect, useMemo, useState} from 'react';
import {useMutation, useQuery} from '@tanstack/react-query';
import {toast} from 'sonner';
import {AlertTriangle, MessageSquare, Smartphone} from 'lucide-react';
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
import {DeviceSelector} from '@/components/DeviceSelector';
import {getApiErrorMessage} from '@/lib/api-error';
import {getAllDevicesStatus} from '@/api/serial';
import {getResetPhone, resetPasswordBySMS} from '@/api/auth';
import type {DeviceStatusSummary} from '@/api/types';

interface SmsResetPasswordCardProps {
    /** 重置成功后回调：页面据此清 token 并跳转登录页 */
    onResetSuccess: (maskedPhone: string, deviceName: string) => void;
}

export function SmsResetPasswordCard({onResetSuccess}: SmsResetPasswordCardProps) {
    const [phone, setPhone] = useState('');
    const [deviceId, setDeviceId] = useState('');
    const [confirmOpen, setConfirmOpen] = useState(false);

    const {data: phoneInfo} = useQuery({
        queryKey: ['reset-phone'],
        queryFn: getResetPhone,
    });

    const {data: devicesData} = useQuery({
        queryKey: ['devices-status'],
        queryFn: getAllDevicesStatus,
    });

    const devices: DeviceStatusSummary[] = devicesData?.devices ?? [];

    // 默认选中第一个在线设备。不静默兜底到「任意设备」——
    // 多卡场景下可能是境外漫游卡，发一条就是国际资费。
    useEffect(() => {
        if (deviceId) return;
        const online = devices.find((d) => d.connected);
        if (online) setDeviceId(online.id);
    }, [devices, deviceId]);

    const selectedDevice = useMemo(
        () => devices.find((d) => d.id === deviceId),
        [devices, deviceId],
    );

    const mutation = useMutation({
        mutationFn: resetPasswordBySMS,
        onSuccess: (res) => {
            setConfirmOpen(false);
            setPhone('');
            toast.success(`新密码已通过短信发送至 ${res.masked_phone}`);
            onResetSuccess(res.masked_phone, res.device_name);
        },
        onError: (error: unknown) => {
            setConfirmOpen(false);
            toast.error(getApiErrorMessage(error, '重置失败'));
        },
    });

    const handleClick = () => {
        if (!deviceId) return toast.warning('请选择发送短信的设备');
        if (!phone.trim() && !phoneInfo?.phone_set)
            return toast.warning('请输入接收短信的手机号');
        setConfirmOpen(true);
    };

    // 已保存号码只以脱敏形式回显，输入框里始终是空的（后端不会给完整号码）
    const targetLabel = phone.trim() || phoneInfo?.masked_phone || '未设置';

    return (
        <>
            <Card className="gap-0 py-0">
                <CardHeader className="border-b border-line-soft py-5">
                    <div>
                        <CardTitle className="flex items-center gap-2 text-base">
                            <MessageSquare className="size-4 text-brand"/>
                            短信重置密码
                        </CardTitle>
                        <p className="mt-1.5 text-xs text-fg-3">
                            生成一个随机新密码并发送到你的手机。新密码只出现在短信里，页面不显示。
                        </p>
                    </div>
                </CardHeader>

                <CardContent className="py-5">
                    <div className="grid max-w-[640px] gap-5">
                        <div className="space-y-1.5">
                            <label
                                htmlFor="reset-phone"
                                className="flex items-center gap-1.5 text-xs font-semibold text-fg-2"
                            >
                                <Smartphone className="size-3.5 text-fg-3"/>
                                接收手机号
                            </label>
                            <Input
                                id="reset-phone"
                                value={phone}
                                onChange={(event) => setPhone(event.target.value)}
                                placeholder={
                                    phoneInfo?.phone_set
                                        ? `已保存 ${phoneInfo.masked_phone}，留空则沿用`
                                        : '如 13800138000'
                                }
                                autoComplete="tel"
                                className="h-11 bg-surface-2 font-mono text-sm focus-visible:bg-surface"
                            />
                            <p className="text-[11px] leading-5 text-fg-3">
                                支持带 <code className="rounded bg-surface-2 px-1">+</code> 的国际区号；留空则使用已保存的号码。
                            </p>
                        </div>

                        <div className="space-y-1.5">
                            <label className="flex items-center gap-1.5 text-xs font-semibold text-fg-2">
                                <MessageSquare className="size-3.5 text-fg-3"/>
                                发送设备
                            </label>
                            <DeviceSelector
                                devices={devices}
                                selectedDeviceId={deviceId}
                                onSelect={setDeviceId}
                                disabled={mutation.isPending}
                            />
                            <p className="text-[11px] leading-5 text-fg-3">
                                短信从所选 SIM 卡发出，请挑一张不会产生国际资费的卡。
                            </p>
                        </div>

                        <div className="flex items-start gap-3 rounded-md border border-err/30 bg-err/10 p-3">
                            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-err"/>
                            <p className="text-xs leading-5 text-err">
                                确认后<strong className="font-semibold">旧密码立即失效</strong>，当前登录也会退出。
                                如果短信发不出去，密码会保持不变 —— 不会把你锁在门外。
                            </p>
                        </div>

                        <div className="border-t border-line-soft pt-5">
                            <Button
                                type="button"
                                onClick={handleClick}
                                disabled={mutation.isPending}
                                className="h-10 bg-brand px-6 text-brand-fg hover:bg-brand"
                            >
                                <MessageSquare className="size-4"/>
                                发送新密码到手机
                            </Button>
                        </div>
                    </div>
                </CardContent>
            </Card>

            <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>确认重置密码？</DialogTitle>
                        <DialogDescription asChild>
                            <div className="space-y-2 pt-1 text-sm text-fg-2">
                                <p>
                                    新密码将发送至{' '}
                                    <span className="font-mono font-semibold text-fg">{targetLabel}</span>
                                </p>
                                <p>
                                    发送设备：{' '}
                                    <span className="font-semibold text-fg">
                                        {selectedDevice?.name || deviceId || '未选择'}
                                    </span>
                                </p>
                                <p className="rounded-md border border-err/30 bg-err/10 p-2.5 text-xs leading-5 text-err">
                                    请确认手机能正常收到短信。密码一旦重置，旧密码不可恢复。
                                </p>
                            </div>
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button
                            variant="outline"
                            onClick={() => setConfirmOpen(false)}
                            disabled={mutation.isPending}
                        >
                            取消
                        </Button>
                        <Button
                            className="bg-err text-white hover:bg-err"
                            disabled={mutation.isPending}
                            onClick={() =>
                                mutation.mutate({
                                    device_id: deviceId,
                                    phone: phone.trim() || undefined,
                                })
                            }
                        >
                            {mutation.isPending && (
                                <span className="size-4 animate-spin rounded-full border-2 border-white/40 border-t-white"/>
                            )}
                            {mutation.isPending ? '发送中...' : '确认重置'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}

