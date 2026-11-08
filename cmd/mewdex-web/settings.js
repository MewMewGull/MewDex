const key = location.hash.slice(1) || sessionStorage.getItem("mewdex.web-panel-key") || "";
if (location.hash) history.replaceState(null, "", location.pathname);
if (key) sessionStorage.setItem("mewdex.web-panel-key", key);
const form = document.getElementById("settings");
const message = document.getElementById("message");
async function request(method, value) {
  const response = await fetch("/__mewdex/config", {
    method,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${key}` },
    body: value && JSON.stringify(value),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}
request("GET").then((value) => {
  document.getElementById("hub").value = value.hubURL;
}).catch((error) => { message.textContent = error.message; });
form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = form.querySelector("button");
  button.disabled = true;
  try {
    await request("POST", { hubURL: document.getElementById("hub").value });
    location.assign("/");
  } catch (error) { message.textContent = error.message; }
  finally { button.disabled = false; }
});
