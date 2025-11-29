import { useState } from "react";

export function InterestSignup({ compact = false }: { compact?: boolean }) {
    const [email, setEmail] = useState("");
    const [status, setStatus] = useState<"idle" | "submitting" | "success" | "error">("idle");

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!email) return;

        setStatus("submitting");
        try {
            const res = await fetch("/api/interest", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ email }),
            });

            if (res.ok) {
                setStatus("success");
                setEmail("");
            } else {
                setStatus("error");
            }
        } catch (err) {
            setStatus("error");
        }
    };

    if (status === "success") {
        return (
            <div className={`text-center text-sm text-green-600 bg-green-50 rounded-md ${compact ? "p-2 text-xs" : "p-3"}`}>
                Thanks! We'll keep you posted.
            </div>
        );
    }

    return (
        <div className={compact ? "px-2" : "text-center"}>
            {!compact && (
                <>
                    <h3 className="text-sm font-medium text-gray-900">Interested in this project?</h3>
                    <p className="text-xs text-gray-500 mt-1 mb-3">
                        Leave your email to get notified when we release.
                    </p>
                </>
            )}
            {compact && (
                <p className="text-xs text-gray-500 mb-2 px-1">
                    Get notified on release:
                </p>
            )}
            <form onSubmit={handleSubmit} className={compact ? "flex flex-col gap-2" : "flex gap-2"}>
                <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="you@example.com"
                    className={`min-w-0 block w-full rounded-md border-gray-300 text-sm border focus:ring-indigo-500 focus:border-indigo-500 ${compact ? "px-2 py-1 text-xs" : "px-3 py-2 flex-1"}`}
                    required
                />
                <button
                    type="submit"
                    disabled={status === "submitting"}
                    className={`inline-flex items-center justify-center border border-transparent font-medium rounded-md text-indigo-700 bg-indigo-100 hover:bg-indigo-200 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 disabled:opacity-50 ${compact ? "px-2 py-1 text-xs w-full" : "px-3 py-2 text-sm leading-4"}`}
                >
                    {status === "submitting" ? "..." : "Notify Me"}
                </button>
            </form>
            {status === "error" && (
                <p className="text-xs text-red-600 mt-2 text-center">Error. Try again?</p>
            )}
        </div>
    );
}
