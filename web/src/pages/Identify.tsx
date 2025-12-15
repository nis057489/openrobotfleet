import { useSearchParams } from "react-router-dom";

export function Identify() {
    const [searchParams] = useSearchParams();
    const id = searchParams.get("id") || "Unknown ID";
    const name = searchParams.get("name") || "Unknown Name";
    const ip = searchParams.get("ip") || "Unknown IP";

    return (
        <div className="min-h-screen bg-black text-white flex flex-col items-center justify-center p-4 text-center">
            <h1 className="text-[15vw] font-bold leading-none mb-4 text-yellow-400">{id}</h1>
            <h2 className="text-[5vw] font-semibold mb-8">{name}</h2>
            <div className="text-[4vw] font-mono bg-gray-800 px-8 py-4 rounded-xl">
                {ip}
            </div>
        </div>
    );
}
