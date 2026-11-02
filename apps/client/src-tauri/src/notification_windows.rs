//! Windows toasts use protocol activation so their links remain usable from
//! Notification Center and after the input process exits. The URI contains only
//! a local random ticket; the shared input still authorizes the target afresh.
use serde::Deserialize;
use std::{
    fs::OpenOptions,
    time::{SystemTime, UNIX_EPOCH},
};
use tauri::Manager;
use windows::{
    core::{Interface, HSTRING},
    Data::Xml::Dom::XmlDocument,
    Foundation::{DateTime, IReference, PropertyValue},
    UI::Notifications::{NotificationSetting, ToastNotification, ToastNotificationManager},
};

use crate::{
    notification_registration_windows::{Apartment, Registration},
    notification_routes::{
        tickets::{self, Store},
        Route,
    },
};

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Notice {
    id: i32,
    title: String,
    body: String,
    extra: serde_json::Value,
}

fn clock() -> Result<u64, String> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|value| value.as_secs())
        .map_err(|_| "无法核对提醒入口的时间".to_owned())
}
fn root(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    app.path()
        .app_local_data_dir()
        .map(|root| root.join("notification-routes"))
        .map_err(|_| "无法定位本机提醒入口".to_owned())
}

fn allowed(registration: &Registration) -> Result<Option<bool>, String> {
    if !registration.ready()? {
        return Ok(None);
    }
    let _apartment = Apartment::enter()?;
    let notifier =
        ToastNotificationManager::CreateToastNotifierWithId(&HSTRING::from(&registration.app_id))
            .map_err(|_| "无法读取系统提醒状态")?;
    Ok(Some(
        notifier.Setting().map_err(|_| "无法读取系统提醒状态")? == NotificationSetting::Enabled,
    ))
}

pub(crate) async fn desktop_notification_permission(
    app: tauri::AppHandle,
) -> Result<Option<bool>, String> {
    tauri::async_runtime::spawn_blocking(move || allowed(&Registration::for_app(&app)?))
        .await
        .map_err(|_| "系统提醒状态读取未完成")?
}

pub(crate) async fn desktop_notification_enable(app: tauri::AppHandle) -> Result<String, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let registration = Registration::for_app(&app)?;
        let directory = root(&app)?;
        let _store = Store::open(directory.clone())?;
        let guard = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(directory.join("registration.lock"))
            .map_err(|_| "无法核对系统提醒关联")?;
        guard
            .try_lock()
            .map_err(|_| "另一个操作正在设置本机提醒，请稍后重试")?;
        registration.install()?;
        Ok(if allowed(&registration)? == Some(true) {
            "granted"
        } else {
            "denied"
        }
        .to_owned())
    })
    .await
    .map_err(|_| "系统提醒设置未完成")?
}

fn text_valid(value: &str, bound: usize) -> bool {
    !value.is_empty() && value.len() <= bound && !value.chars().any(|c| c.is_control())
}
fn xml(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

fn content(scheme: &str, uri: &str, title: &str, body: &str) -> Result<String, String> {
    if !text_valid(title, 512)
        || !text_valid(body, 2048)
        || tickets::parse_activation(scheme, uri).is_none()
    {
        return Err("提醒内容或对话入口无效".into());
    }
    let uri = xml(uri);
    Ok(format!("<toast activationType=\"protocol\" launch=\"{uri}\"><visual><binding template=\"ToastGeneric\"><text>{}</text><text>{}</text></binding></visual><actions><action content=\"打开对话\" activationType=\"protocol\" arguments=\"{uri}\"/></actions></toast>", xml(title), xml(body)))
}

pub(crate) async fn desktop_notification_send(
    app: tauri::AppHandle,
    options: Notice,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let registration = Registration::for_app(&app)?;
        if allowed(&registration)? != Some(true) {
            return Err("系统提醒尚未开启，请检查此输入端的提醒设置".into());
        }
        let route = Route::from_extra(&options.extra).ok_or("提醒缺少有效的对话入口")?;
        if !text_valid(&options.title, 512) || !text_valid(&options.body, 2048) {
            return Err("提醒内容无效".into());
        }
        let store = Store::open(root(&app)?)?;
        let (ticket, retired) = store.create(clock()?, options.id, route)?;
        let result = (|| {
            let _apartment = Apartment::enter()?;
            let document = XmlDocument::new().map_err(|_| "无法准备系统提醒")?;
            document
                .LoadXml(&HSTRING::from(content(
                    &registration.scheme,
                    &tickets::activation_url(&registration.scheme, &ticket.token)?,
                    &options.title,
                    &options.body,
                )?))
                .map_err(|_| "无法准备系统提醒")?;
            let toast = ToastNotification::CreateToastNotification(&document)
                .map_err(|_| "无法准备系统提醒")?;
            toast
                .SetTag(&HSTRING::from(&ticket.token[..16]))
                .map_err(|_| "无法标识系统提醒")?;
            toast
                .SetGroup(&HSTRING::from("MewDex"))
                .map_err(|_| "无法标识系统提醒")?;
            let notifier = ToastNotificationManager::CreateToastNotifierWithId(&HSTRING::from(
                &registration.app_id,
            ))
            .map_err(|_| "系统未接收本次提醒")?;
            if let Ok(history) = ToastNotificationManager::History() {
                for old in retired {
                    let _ = history.RemoveGroupedTagWithId(
                        &HSTRING::from(&old.token[..16]),
                        &HSTRING::from("MewDex"),
                        &HSTRING::from(&registration.app_id),
                    );
                }
            }
            let expiry = ticket
                .expires_at()
                .checked_add(11_644_473_600)
                .and_then(|value| value.checked_mul(10_000_000))
                .and_then(|value| i64::try_from(value).ok())
                .ok_or("无法核对提醒的有效期")?;
            let expiry: IReference<DateTime> = PropertyValue::CreateDateTime(DateTime {
                UniversalTime: expiry,
            })
            .and_then(|value| value.cast())
            .map_err(|_| "无法设置提醒的有效期")?;
            toast
                .SetExpirationTime(&expiry)
                .map_err(|_| "无法设置提醒的有效期")?;
            notifier.Show(&toast).map_err(|_| "系统未接收本次提醒")?;
            Ok(())
        })();
        if result.is_err() {
            let _ = store.remove(&ticket.token);
        }
        result
    })
    .await
    .map_err(|_| "系统提醒投递未完成")?
}

pub(crate) fn activate(app: &tauri::AppHandle, args: impl IntoIterator<Item = String>) {
    let values: Vec<_> = args
        .into_iter()
        .filter_map(|argument| tickets::parse_activation(tickets::SCHEME, &argument))
        .take(2)
        .collect();
    if values.len() != 1 {
        return;
    }
    let result = (|| {
        let store = Store::open(root(app)?)?;
        let ticket = store.resolve(clock()?, &values[0])?;
        if app
            .state::<crate::notification_actions::Inbox>()
            .record_route(ticket.notification_id, ticket.route)
        {
            use tauri::Emitter;
            let _ = app.emit_to("main", crate::notification_actions::EVENT, ());
        }
        Ok::<(), String>(())
    })();
    #[cfg(debug_assertions)]
    crate::diagnostics::emit(
        "desktop-notification-activation",
        serde_json::json!({"ticketFound": result.is_ok()}),
    );
    #[cfg(not(debug_assertions))]
    let _ = result;
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn native_content_escapes_text_and_carries_only_an_opaque_uri() {
        let uri = tickets::activation_url(tickets::SCHEME, &"b".repeat(64)).unwrap();
        let body = content(tickets::SCHEME, &uri, "MewDex <&>", "一轮对话已完成。").unwrap();
        assert!(body.contains("MewDex &lt;&amp;&gt;"));
        assert!(body.contains("activationType=\"protocol\""));
        assert!(!body.contains("deviceId") && !body.contains("scope"));
        assert!(content(
            tickets::SCHEME,
            "https://external.invalid",
            "MewDex",
            "summary"
        )
        .is_err());
        assert!(content(tickets::SCHEME, &uri, "MewDex", "bad\0text").is_err());
    }
}
