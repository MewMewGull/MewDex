import { Network } from "lucide-react";

export function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <div className="brand">
      {!compact && (
        <span className="brand-mark">
          <Network size={20} />
        </span>
      )}
      <strong>MewDex</strong>
    </div>
  );
}
