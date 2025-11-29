import { useEffect, useState } from "react";
import { Outlet, useNavigate } from "react-router-dom";

export function AuthGuard() {
    const [isLoading, setIsLoading] = useState(true);
    const navigate = useNavigate();

    useEffect(() => {
        const checkAuth = async () => {
            try {
                const res = await fetch("/api/auth/status");
                if (res.ok) {
                    setIsLoading(false);
                } else {
                    // If 401 or other error, redirect
                    navigate("/login");
                }
            } catch (err) {
                navigate("/login");
            }
        };

        checkAuth();
    }, [navigate]);

    if (isLoading) {
        return (
            <div className="min-h-screen flex items-center justify-center bg-gray-50">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
            </div>
        );
    }

    return <Outlet />;
}
