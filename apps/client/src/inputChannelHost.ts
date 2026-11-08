// Ordering belongs to the browser tab so a late refresh cannot undo sign-out.
export function nextHostedChannelRequest() {
  let clientId = sessionStorage.getItem("mewdex.channel-client");
  if (!clientId) {
    clientId = crypto.randomUUID();
    sessionStorage.setItem("mewdex.channel-client", clientId);
  }
  const epoch =
    Number(sessionStorage.getItem("mewdex.channel-epoch") || "0") + 1;
  sessionStorage.setItem("mewdex.channel-epoch", String(epoch));
  return { clientId, epoch };
}
