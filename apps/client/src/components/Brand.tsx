const inputIcon = new URL("../assets/input-icon.svg", import.meta.url).href;

export function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <div className="brand">
      {!compact && (
        <img className="brand-mark" src={inputIcon} alt="" aria-hidden="true" />
      )}
      <strong>MewDex</strong>
    </div>
  );
}
