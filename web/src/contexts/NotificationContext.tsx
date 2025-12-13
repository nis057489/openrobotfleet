import React, { createContext, useContext, useState, useCallback, ReactNode } from 'react';
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react';

export type NotificationType = 'success' | 'error' | 'info';

export interface Notification {
    id: string;
    type: NotificationType;
    message: string;
    duration?: number;
}

interface NotificationContextType {
    notify: (type: NotificationType, message: string, duration?: number) => void;
    success: (message: string, duration?: number) => void;
    error: (message: string, duration?: number) => void;
    info: (message: string, duration?: number) => void;
}

const NotificationContext = createContext<NotificationContextType | undefined>(undefined);

export const useNotification = () => {
    const context = useContext(NotificationContext);
    if (!context) {
        throw new Error('useNotification must be used within a NotificationProvider');
    }
    return context;
};

export const NotificationProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const [notifications, setNotifications] = useState<Notification[]>([]);

    const removeNotification = useCallback((id: string) => {
        setNotifications(prev => prev.filter(n => n.id !== id));
    }, []);

    const notify = useCallback((type: NotificationType, message: string, duration = 5000) => {
        const id = Math.random().toString(36).substring(2, 9);
        setNotifications(prev => [...prev, { id, type, message, duration }]);

        if (duration > 0) {
            setTimeout(() => {
                removeNotification(id);
            }, duration);
        }
    }, [removeNotification]);

    const success = useCallback((message: string, duration?: number) => notify('success', message, duration), [notify]);
    const error = useCallback((message: string, duration?: number) => notify('error', message, duration), [notify]);
    const info = useCallback((message: string, duration?: number) => notify('info', message, duration), [notify]);

    return (
        <NotificationContext.Provider value={{ notify, success, error, info }}>
            {children}
            <div className="fixed top-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
                {notifications.map(notification => (
                    <div
                        key={notification.id}
                        className={`
                            pointer-events-auto
                            flex items-center gap-3 px-4 py-3 rounded-lg shadow-lg border min-w-[300px] max-w-md
                            transform transition-all duration-300 ease-in-out animate-in slide-in-from-right-full fade-in
                            ${notification.type === 'success' ? 'bg-white border-green-200 text-green-800' : ''}
                            ${notification.type === 'error' ? 'bg-white border-red-200 text-red-800' : ''}
                            ${notification.type === 'info' ? 'bg-white border-blue-200 text-blue-800' : ''}
                        `}
                    >
                        <div className="flex-shrink-0">
                            {notification.type === 'success' && <CheckCircle size={20} className="text-green-500" />}
                            {notification.type === 'error' && <AlertCircle size={20} className="text-red-500" />}
                            {notification.type === 'info' && <Info size={20} className="text-blue-500" />}
                        </div>
                        <p className="text-sm font-medium flex-1">{notification.message}</p>
                        <button
                            onClick={() => removeNotification(notification.id)}
                            className="text-gray-400 hover:text-gray-600 transition-colors"
                        >
                            <X size={16} />
                        </button>
                    </div>
                ))}
            </div>
        </NotificationContext.Provider>
    );
};
