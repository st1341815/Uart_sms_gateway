import {useState} from 'react';
import {useNavigate} from 'react-router-dom';
import {Eye, EyeOff, KeyRound, Lock, ShieldCheck, User} from 'lucide-react';
import {useMutation, useQuery} from '@tanstack/react-query';
import {toast} from 'sonner';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from '@/components/ui/card';
import {changePassword, getMe} from '../api/auth';

// 口令强度实时提示
function strengthOf(pw: string): { level: number; text: string; cls: string } {
    let score = 0;
    if (pw.length >= 8) score++;
    if (pw.length >= 12) score++;
    if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++;
    if (/\d/.test(pw)) score++;
    if (/[^a-zA-Z0-9]/.test(pw)) score++;

    if (pw.length === 0) return {level: 0, text: '', cls: ''};
    if (score <= 2) return {level: 1, text: '弱', cls: 'bg-red-500'};
    if (score <= 3) return {level: 2, text: '中', cls: 'bg-amber-500'};
    return {level: 3, text: '强', cls: 'bg-green-500'};
}

export default function ChangePassword() {
    const navigate = useNavigate();
    const [oldPassword, setOldPassword] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [confirmPassword, setConfirmPassword] = useState('');

    const [showOld, setShowOld] = useState(false);
    const [showNew, setShowNew] = useState(false);
    const [showConfirm, setShowConfirm] = useState(false);

    const {data: me} = useQuery({
        queryKey: ['me'],
        queryFn: getMe,
    });

    const mutation = useMutation({
        mutationFn: changePassword,
        onSuccess: () => {
            toast.success('密码修改成功，请使用新密码重新登录');
            // 清除本地 token，强制重新登录
            localStorage.removeItem('token');
            localStorage.removeItem('expiresAt');
            setTimeout(() => navigate('/login', {replace: true}), 800);
        },
        onError: (error: any) => {
            // apiClient 在非 2xx 时把响应体原文塞进 Error.message，
            // 后端返回的是 {"error":"..."}，这里尽量解析出可读文案
            let msg = error?.message || '修改失败';
            try {
                const parsed = JSON.parse(msg);
                if (parsed?.error) msg = parsed.error;
            } catch {
                /* 不是 JSON 就原样使用 */
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

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (!oldPassword) return toast.warning('请输入当前密码');
        if (newPassword.length < 8) return toast.warning('新密码长度至少 8 位');
        if (!/[a-zA-Z]/.test(newPassword) || !/\d/.test(newPassword))
            return toast.warning('新密码需同时包含字母和数字');
        if (newPassword !== confirmPassword) return toast.warning('两次输入的新密码不一致');
        if (newPassword === oldPassword) return toast.warning('新密码不能与当前密码相同');

        mutation.mutate({old_password: oldPassword, new_password: newPassword});
    };

    return (
        <div className="max-w-2xl mx-auto">
            <div className="mb-6">
                <h1 className="text-2xl font-bold text-gray-900 flex items-center gap-2">
                    <ShieldCheck className="w-6 h-6 text-blue-600"/>
                    账号安全
                </h1>
                <p className="text-sm text-gray-500 mt-1">修改管理员登录密码</p>
            </div>

            <Card>
                <CardHeader>
                    <CardTitle className="text-lg flex items-center gap-2">
                        <KeyRound className="w-5 h-5 text-gray-500"/>
                        修改密码
                    </CardTitle>
                    <CardDescription>
                        当前登录账号：
                        <span className="font-mono font-semibold text-gray-700 ml-1">
                            {me?.username || '—'}
                        </span>
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <form onSubmit={handleSubmit} className="space-y-5">
                        {/* 当前密码 */}
                        <div>
                            <label className="block text-xs font-semibold text-gray-600 mb-2 uppercase tracking-wide flex items-center gap-1.5">
                                <Lock size={12} className="text-gray-400"/>
                                当前密码 <span className="text-red-500">*</span>
                            </label>
                            <div className="relative">
                                <Input
                                    type={showOld ? 'text' : 'password'}
                                    value={oldPassword}
                                    onChange={(e) => setOldPassword(e.target.value)}
                                    placeholder="请输入当前密码"
                                    autoComplete="current-password"
                                    className="bg-gray-50 border-gray-200 focus:bg-white focus:border-blue-500 pr-10"
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowOld(!showOld)}
                                    className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                                    tabIndex={-1}
                                >
                                    {showOld ? <EyeOff className="w-4 h-4"/> : <Eye className="w-4 h-4"/>}
                                </button>
                            </div>
                        </div>

                        {/* 新密码 */}
                        <div>
                            <label className="block text-xs font-semibold text-gray-600 mb-2 uppercase tracking-wide flex items-center gap-1.5">
                                <KeyRound size={12} className="text-gray-400"/>
                                新密码 <span className="text-red-500">*</span>
                            </label>
                            <div className="relative">
                                <Input
                                    type={showNew ? 'text' : 'password'}
                                    value={newPassword}
                                    onChange={(e) => setNewPassword(e.target.value)}
                                    placeholder="至少 8 位，需含字母和数字"
                                    autoComplete="new-password"
                                    className="bg-gray-50 border-gray-200 focus:bg-white focus:border-blue-500 pr-10"
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowNew(!showNew)}
                                    className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                                    tabIndex={-1}
                                >
                                    {showNew ? <EyeOff className="w-4 h-4"/> : <Eye className="w-4 h-4"/>}
                                </button>
                            </div>
                            {/* 强度条 */}
                            {newPassword.length > 0 && (
                                <div className="mt-2 flex items-center gap-2">
                                    <div className="flex-1 flex gap-1">
                                        {[1, 2, 3].map((i) => (
                                            <div
                                                key={i}
                                                className={`h-1 flex-1 rounded-full transition-colors ${
                                                    strength.level >= i ? strength.cls : 'bg-gray-200'
                                                }`}
                                            />
                                        ))}
                                    </div>
                                    <span className="text-xs text-gray-500 w-8 text-right">{strength.text}</span>
                                </div>
                            )}
                            <p className="text-xs text-gray-400 mt-1.5">
                                至少 8 位、最多 72 位，需同时包含字母和数字
                            </p>
                        </div>

                        {/* 确认新密码 */}
                        <div>
                            <label className="block text-xs font-semibold text-gray-600 mb-2 uppercase tracking-wide flex items-center gap-1.5">
                                <KeyRound size={12} className="text-gray-400"/>
                                确认新密码 <span className="text-red-500">*</span>
                            </label>
                            <div className="relative">
                                <Input
                                    type={showConfirm ? 'text' : 'password'}
                                    value={confirmPassword}
                                    onChange={(e) => setConfirmPassword(e.target.value)}
                                    placeholder="请再次输入新密码"
                                    autoComplete="new-password"
                                    className={`bg-gray-50 border-gray-200 focus:bg-white pr-10 ${
                                        mismatch ? 'border-red-400 focus:border-red-500' : 'focus:border-blue-500'
                                    }`}
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowConfirm(!showConfirm)}
                                    className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                                    tabIndex={-1}
                                >
                                    {showConfirm ? <EyeOff className="w-4 h-4"/> : <Eye className="w-4 h-4"/>}
                                </button>
                            </div>
                            {mismatch && (
                                <p className="text-xs text-red-500 mt-1.5">两次输入的新密码不一致</p>
                            )}
                        </div>

                        <div className="flex items-center gap-3 pt-2">
                            <Button type="submit" disabled={!canSubmit} className="px-6">
                                {mutation.isPending ? '提交中...' : '确认修改'}
                            </Button>
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => {
                                    setOldPassword('');
                                    setNewPassword('');
                                    setConfirmPassword('');
                                }}
                            >
                                清空
                            </Button>
                        </div>
                    </form>
                </CardContent>
            </Card>

            <div className="mt-4 p-4 bg-amber-50 border border-amber-200 rounded-lg">
                <p className="text-xs text-amber-800 leading-relaxed">
                    <strong>提示：</strong>修改成功后需要重新登录。旧密码将立即失效。
                    若忘记当前密码，可在服务器上编辑 <code className="bg-amber-100 px-1 rounded">config.yaml</code>
                    的 <code className="bg-amber-100 px-1 rounded">Users</code> 字段重新初始化
                    （仅当数据库中该用户记录不存在时才会生效）。
                </p>
            </div>
        </div>
    );
}
