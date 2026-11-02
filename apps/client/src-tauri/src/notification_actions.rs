//! Keep a bounded native navigation hint while the input webview starts/resumes.
//! A hint never grants access; the input must authorize its conversation afresh.
use serde::Serialize;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Mutex,
};
use tauri::{Emitter, Manager};
use tauri_plugin_notification::{ActionPerformed, NotificationExt};

pub(crate) const EVENT: &str = "mewdex://notification-action";

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Hint {
    action_id: String,
    notification_id: i32,
    scope: String,
    device_id: String,
    session_id: String,
}

impl Hint {
    fn from_action(action: &ActionPerformed) -> Option<Self> {
        if !matches!(action.action_id(), "tap" | "open") {
            return None;
        }
        let value = serde_json::to_value(action).ok()?;
        let notification = value.get("notification")?;
        let extra = notification.get("extra")?;
        let route = crate::notification_routes::Route::from_extra(extra)?;
        Some(Self {
            action_id: action.action_id().to_owned(),
            notification_id: i32::try_from(notification.get("id")?.as_i64()?).ok()?,
            scope: route.scope,
            device_id: route.device_id,
            session_id: route.session_id,
        })
    }
}

#[derive(Default)]
pub(crate) struct Inbox {
    ready: AtomicBool,
    latest: Mutex<Option<Hint>>,
}

impl Inbox {
    #[cfg(target_os = "windows")]
    pub(crate) fn record_route(
        &self,
        notification_id: i32,
        route: crate::notification_routes::Route,
    ) -> bool {
        if !route.valid() {
            return false;
        }
        let Ok(mut latest) = self.latest.lock() else {
            return false;
        };
        *latest = Some(Hint {
            action_id: "tap".into(),
            notification_id,
            scope: route.scope,
            device_id: route.device_id,
            session_id: route.session_id,
        });
        true
    }
    fn record(&self, action: &ActionPerformed) -> bool {
        let Some(hint) = Hint::from_action(action) else {
            return false;
        };
        let Ok(mut latest) = self.latest.lock() else {
            return false;
        };
        *latest = Some(hint);
        true
    }

    fn take(&self) -> Result<Option<Hint>, String> {
        if !self.ready.load(Ordering::Acquire) {
            return Err("系统提醒的对话入口暂时不可用".into());
        }
        self.latest
            .lock()
            .map(|mut latest| latest.take())
            .map_err(|_| "提醒入口正在忙碌，请重试".into())
    }
}

pub(crate) fn install(app: &tauri::AppHandle) {
    let handle = app.clone();
    let result = app.notification().on_action(move |action| {
        if handle.state::<Inbox>().record(action) {
            // The event contains no metadata. Read-and-consume is scoped to
            // the application's native command, including after suspension.
            let _ = handle.emit_to("main", EVENT, ());
        }
    });
    app.state::<Inbox>()
        .ready
        .store(result.is_ok(), Ordering::Release);
}

#[tauri::command]
pub(crate) fn notification_take_action(app: tauri::AppHandle) -> Result<Option<Hint>, String> {
    app.state::<Inbox>().take()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn action(id: i32, session: &str, kind: &str) -> ActionPerformed {
        serde_json::from_value(json!({
            "actionId": kind,
            "notification": {"id": id, "title": "MewDex", "body": "private body",
                "extra": {"scope": "scope", "deviceId": "node", "sessionId": session,
                    "privateAnswer": "never cache this"}}
        }))
        .unwrap()
    }

    #[test]
    fn native_startup_retains_only_the_latest_hint_and_consumes_it_once() {
        let inbox = Inbox::default();
        assert!(inbox.record(&action(1, "first", "tap")));
        assert!(inbox.record(&action(2, "latest", "open")));
        assert!(inbox.take().is_err());
        inbox.ready.store(true, Ordering::Release);
        let hint = inbox.take().unwrap().unwrap();
        assert_eq!(hint.session_id, "latest");
        assert_eq!(hint.notification_id, 2);
        let raw = serde_json::to_string(&hint).unwrap();
        assert!(!raw.contains("private"));
        assert!(inbox.take().unwrap().is_none());
    }

    #[test]
    fn dismissal_and_incomplete_or_oversized_metadata_do_not_replace_a_hint() {
        let inbox = Inbox::default();
        inbox.ready.store(true, Ordering::Release);
        assert!(inbox.record(&action(1, "keep", "tap")));
        assert!(!inbox.record(&action(2, "ignored", "dismiss")));
        assert!(!inbox.record(&action(3, &"x".repeat(513), "tap")));
        let missing: ActionPerformed = serde_json::from_value(json!({"actionId":"tap"})).unwrap();
        assert!(!inbox.record(&missing));
        assert_eq!(inbox.take().unwrap().unwrap().session_id, "keep");
    }
}
