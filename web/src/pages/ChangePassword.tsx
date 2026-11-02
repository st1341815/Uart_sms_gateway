import {useState} from 'react';
import {useNavigate} from 'react-router-dom';
import {Eye, EyeOff, Info, KeyRound, Lock, ShieldCheck, UserRound} from 'lucide-react';
import {useMutation, useQuery} from '@tanstack/react-query';
import {toast} from 'sonner';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {PageHeader} from '@/components/PageHeader';
import {changePassword, getMe} from '../api/auth';
import {cn} from '@/lib/utils';

/** 口令强度实时评估 */
function strengthOf(pw: string): {level: number; text: string; tone: string} {
    let score = 0;
    if (pw.length >= 8) score++;
    if (pw.length >= 12) score++;
    if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++;
    if (/\d/.test(pw)) score++;
    if (/[^a-zA-Z0-9]/.test(pw)) score++;

    if (pw.length === 0) return {level: 0, text: '', tone: ''};
    if (score <= 2) return {level: 1, text: '弱', tone: 'bg-rose-500'};
    if (score <= 3) return {level: 2, text: '中', tone: 'bg-amber-500'};
    return {level: 3, text: '强', tone: 'bg-emerald-500'};
}

interface PasswordFieldProps {
    id: string;
    label: string;
    value: string;
    onChange: (value: string) => void;
    placeholder: string;
    autoComplete: string;
    invalid?: boolean;
    icon?: 'lock' | 'key';
}

function PasswordField({id, label, value, onChange, placeholder, autoComplete, invalid, icon = 'key'}: PasswordFieldProps) {
    const [visible, setVisible] = useState(false);
    const Icon = icon === 'lock' ? Lock : KeyRound;

    return (
        <div className="space-y-1.5">
            <label htmlFor={id} className="flex items-center gap-1.5 text-xs font-semibold text-slate-700">
                <Icon className="size-3.5 text-slate-400"/>
                {label}
                <span className="text-rose-500">*</span>
            </label>
            <div className="relative">
                <Input
                    id={id}
                    type={visible ? 'text' : 'password'}
                    value={value}
                    onChange={(event) => onChange(event.target.value)}
                    placeholder={placeholder}
                    autoComplete={autoComplete}
                    className={cn(
                        'h-11 bg-slate-50 pr-10 font-mono text-sm focus-visible:bg-white',
                        invalid ? 'border-rose-400 focus-visible:border-rose-500' : 'focus-visible:border-blue-500',
                    )}
                />
                <button
                    type="button"
                    onClick={() => setVisible(!visible)}
                    tabIndex={-1}
                    aria-label={visible ? '隐藏密码' : '显示密码'}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 transition hover:text-slate-600"
                >
                    {visible ? <EyeOff className="size-4"/> : <Eye className="size-4"/>}
                </button>
            </div>
        </div>
    );
}

export default function ChangePassword() {
    const navigate = useNavigate();
    const [oldPassword, setOldPassword] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [confirmPassword, setConfirmPassword] = useState('');

    const {data: me} = useQuery({
        queryKey: ['me'],
        queryFn: getMe,
    });

    const mutation = useMutation({
        mutationFn: changePassword,
        onSuccess: () => {
            toast.success('密码修改成功，请使用新密码重新登录');
            localStorage.removeItem('token');
            localStorage.removeItem('expiresAt');
            setTimeout(() => navigate('/login', {replace: true}), 800);
        },
        onError: (error: unknown) => {
            // apiClient 在非 2xx 时把响应体原文塞进 Error.message，
            // 后端返回 {"error":"..."}，这里尽量解析出可读文案
            let msg = error instanceof Error ? error.message : '修改失败';
            try {
                const parsed = JSON.parse(msg);
                if (parsed?.error) msg = parsed.error;
            } catch {
                /* 非 JSON 原样使用 */
            }
            toast.error(msg);
        },
    });

    const strength = strengthOf(newPassword);
    const mismatch = confirmPassword.length > 0 && newPassword !== confirmPassword;
    const canSubmit =
        oldPassword.length > 0 &&
        newPassword.length > 0 &&
        confirmPassword.length > 0 &&
        !mismatch &&
        !mutation.isPending;

    const handleSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        if (!oldPassword) return toast.warning('请输入当前密码');
        if (newPassword.length < 8) return toast.warning('新密码长度至少 8 位');
        if (!/[a-zA-Z]/.test(newPassword) || !/\d/.test(newPassword))
            return toast.warning('新密码需同时包含字母和数字');
        if (newPassword !== confirmPassword) return toast.warning('两次输入的新密码不一致');
        if (newPassword === oldPassword) return toast.warning('新密码不能与当前密码相同');

        mutation.mutate({old_password: oldPassword, new_password: newPassword});
    };

    const resetAll = () => {
        setOldPassword('');
        setNewPassword('');
        setConfirmPassword('');
    };

    return (
        <div>
            <PageHeader
                title="账号安全"
                description="修改管理员登录密码。修改成功后需要重新登录，旧密码立即失效。"
            />

            <div className="mt-6 grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(280px,0.55fr)]">
                <Card className="gap-0 py-0">
                    <CardHeader className="border-b border-slate-100 py-5">
                        <div className="flex items-center justify-between gap-4">
                            <div>
                                <CardTitle className="flex items-center gap-2 text-base">
                                    <ShieldCheck className="size-4 text-blue-600"/>
                                    修改密码
                                </CardTitle>
                                <p className="mt-1.5 text-xs text-slate-500">至少 8 位、最多 72 位，需同时包含字母和数字</p>
                            </div>
                            <div className="flex shrink-0 items-center gap-2 rounded-full border border-slate-200 bg-slate-50 px-3 py-1.5 text-xs">
                                <UserRound className="size-3.5 text-slate-400"/>
                                <span className="font-mono font-semibold text-slate-700">{me?.username || '—'}</span>
                            </div>
                        </div>
                    </CardHeader>

                    <CardContent className="py-5">
                        <form onSubmit={handleSubmit} className="grid max-w-[640px] gap-5">
                            <PasswordField
                                id="old-password"
                                label="当前密码"
                                value={oldPassword}
                                onChange={setOldPassword}
                                placeholder="请输入当前密码"
                                autoComplete="current-password"
                                icon="lock"
                            />

                            <div>
                                <PasswordField
                                    id="new-password"
                                    label="新密码"
                                    value={newPassword}
                                    onChange={setNewPassword}
                                    placeholder="至少 8 位，需含字母和数字"
                                    autoComplete="new-password"
                                />
                                {newPassword.length > 0 && (
                                    <div className="mt-2 flex items-center gap-2">
                                        <div className="flex flex-1 gap-1">
                                            {[1, 2, 3].map((step) => (
                                                <div
                                                    key={step}
                                                    className={cn(
                                                        'h-1 flex-1 rounded-full transition-colors',
                                                        strength.level >= step ? strength.tone : 'bg-slate-200',
                                                    )}
                                                />
                                            ))}
                                        </div>
                                        <span className="w-8 text-right text-[10px] font-semibold text-slate-500">
                                            {strength.text}
                                        </span>
                                    </div>
                                )}
                            </div>

                            <div>
                                <PasswordField
                                    id="confirm-password"
                                    label="确认新密码"
                                    value={confirmPassword}
                                    onChange={setConfirmPassword}
                                    placeholder="请再次输入新密码"
                                    autoComplete="new-password"
                                    invalid={mismatch}
                                />
                                {mismatch && (
                                    <p className="mt-1.5 text-xs text-rose-500">两次输入的新密码不一致</p>
                                )}
                            </div>

                            <div className="flex items-center gap-3 border-t border-slate-100 pt-5">
                                <Button
                                    type="submit"
                                    disabled={!canSubmit}
                                    className="h-10 bg-blue-600 px-6 text-white hover:bg-blue-700"
                                >
                                    {mutation.isPending && <span className="size-4 animate-spin rounded-full border-2 border-white/40 border-t-white"/>}
                                    {mutation.isPending ? '提交中...' : '确认修改'}
                                </Button>
                                <Button type="button" variant="outline" onClick={resetAll} className="h-10">
                                    清空
                                </Button>
                            </div>
                        </form>
                    </CardContent>
                </Card>

                <div className="grid gap-4">
                    <Card className="gap-0 py-0">
                        <CardHeader className="border-b border-slate-100 py-4">
                            <CardTitle className="text-sm">密码要求</CardTitle>
                        </CardHeader>
                        <CardContent className="py-4">
                            <ul className="space-y-2.5 text-xs leading-5 text-slate-600">
                                {[
                                    '长度至少 8 位，最多 72 位',
                                    '必须同时包含字母和数字',
                                    '不能与当前密码相同',
                                ].map((rule) => (
                                    <li key={rule} className="flex items-start gap-2">
                                        <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-blue-500"/>
                                        {rule}
                                    </li>
                                ))}
                            </ul>
                        </CardContent>
                    </Card>

                    <div className="flex gap-3 rounded-2xl border border-amber-200 bg-amber-50/70 p-4">
                        <Info className="mt-0.5 size-4 shrink-0 text-amber-600"/>
                        <div className="text-xs leading-6 text-amber-900">
                            <p className="font-semibold">忘记当前密码怎么办</p>
                            <p className="mt-1 text-amber-800">
                                可在服务器上编辑 <code className="rounded bg-amber-100 px-1">config.yaml</code> 的{' '}
                                <code className="rounded bg-amber-100 px-1">Users</code> 字段重新初始化
                                （仅当数据库中该用户记录不存在时才会生效）。
                            </p>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
