import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import type { DeviceStatusSummary } from '@/api/types';

interface DeviceSelectorProps {
    devices: DeviceStatusSummary[];
    selectedDeviceId: string;
    onSelect: (deviceId: string) => void;
    disabled?: boolean;
}

export function DeviceSelector({ 
    devices, 
    selectedDeviceId, 
    onSelect, 
    disabled = false 
}: DeviceSelectorProps) {
    const selectedDevice = devices.find(d => d.id === selectedDeviceId);

    return (
        <Select
            value={selectedDeviceId}
            onValueChange={onSelect}
            disabled={disabled}
        >
            <SelectTrigger className="w-full h-9">
                <SelectValue placeholder="选择设备">
                    {selectedDevice && (
                        <div className="flex items-center gap-2">
                            <div className={cn(
                                "w-1.5 h-1.5 rounded-full",
                                selectedDevice.connected ? "bg-green-600" : "bg-red-600"
                            )} />
                            <span className="truncate">{selectedDevice.name}</span>
                            <span className="text-xs text-gray-400">
                                ({selectedDevice.connected ? '在线' : '离线'})
                            </span>
                        </div>
                    )}
                </SelectValue>
            </SelectTrigger>
            <SelectContent>
                {devices.map((device) => (
                    <SelectItem 
                        key={device.id} 
                        value={device.id}
                    >
                        <div className="flex items-center gap-2">
                            <div className={cn(
                                "w-1.5 h-1.5 rounded-full",
                                device.connected ? "bg-green-600" : "bg-red-600"
                            )} />
                            <span className="font-medium">{device.name}</span>
                            <span className="text-xs text-gray-400">
                                ({device.connected ? '在线' : '离线'})
                            </span>
                            <span className="text-xs text-gray-400 font-mono ml-auto">
                                {device.operator || '-'}
                            </span>
                        </div>
                    </SelectItem>
                ))}
            </SelectContent>
        </Select>
    );
}