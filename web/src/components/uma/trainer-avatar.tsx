import { useState } from "react";

export function TrainerAvatar({ id, size = 56, loading = "lazy" }: { id: string; size?: number; loading?: "eager" | "lazy" }) {
  return <Portrait key={id} id={id} size={size} loading={loading} />;
}
function Portrait({ id, size, loading }: { id: string; size: number; loading: "eager" | "lazy" }) {
  const [failed, setFailed] = useState(false);
  return <img src={failed ? "/assets/ui/branding/party-logo.webp" : `/assets/ui/avatars/${id}.webp`} width={size} height={size} alt="" loading={loading} decoding="async" onError={failed ? undefined : () => setFailed(true)} />;
}
