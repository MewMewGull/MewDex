//! Native notification links carry an opaque local ticket, never credentials or
//! model text. Resolving a ticket yields only a navigation hint; the input must
//! still reauthorize the conversation through its current network identity.
use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Debug, Deserialize, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Route {
    pub(crate) scope: String,
    pub(crate) device_id: String,
    pub(crate) session_id: String,
}

impl Route {
    pub(crate) fn valid(&self) -> bool {
        [
            (&self.scope, 4096),
            (&self.device_id, 512),
            (&self.session_id, 512),
        ]
        .into_iter()
        .all(|(value, limit)| {
            !value.is_empty() && value.len() <= limit && !value.chars().any(char::is_control)
        })
    }

    pub(crate) fn from_extra(extra: &Value) -> Option<Self> {
        let route = Self {
            scope: extra.get("scope")?.as_str()?.to_owned(),
            device_id: extra.get("deviceId")?.as_str()?.to_owned(),
            session_id: extra.get("sessionId")?.as_str()?.to_owned(),
        };
        route.valid().then_some(route)
    }
}

#[cfg(desktop)]
pub(crate) mod tickets {
    use super::Route;
    use serde::{Deserialize, Serialize};
    use std::{
        fs::{self, File, OpenOptions},
        io::{Read, Write},
        path::PathBuf,
    };

    const TTL: u64 = 7 * 24 * 60 * 60;
    const MAX_TICKETS: usize = 256;
    const MAX_ENTRIES: usize = 1024;
    const MAX_FILE: u64 = 32768;
    pub(crate) const SCHEME: &str = "mewdex-notification";

    #[derive(Clone, Debug, Deserialize, Serialize)]
    #[serde(rename_all = "camelCase", deny_unknown_fields)]
    pub(crate) struct Ticket {
        format: u32,
        pub(crate) token: String,
        created_at: u64,
        pub(crate) notification_id: i32,
        pub(crate) route: Route,
    }

    impl Ticket {
        pub(crate) fn expires_at(&self) -> u64 {
            self.created_at.saturating_add(TTL)
        }
    }

    pub(crate) struct Store {
        root: PathBuf,
        capacity: usize,
    }

    pub(crate) fn token_valid(token: &str) -> bool {
        token.len() == 64
            && token
                .bytes()
                .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
    }

    pub(crate) fn scheme_valid(scheme: &str) -> bool {
        !scheme.is_empty()
            && scheme.len() <= 80
            && scheme.as_bytes()[0].is_ascii_lowercase()
            && scheme
                .bytes()
                .all(|v| v.is_ascii_lowercase() || v.is_ascii_digit() || b".-".contains(&v))
    }

    pub(crate) fn activation_url(scheme: &str, token: &str) -> Result<String, String> {
        if !scheme_valid(scheme) || !token_valid(token) {
            return Err("invalid notification link".into());
        }
        Ok(format!("{scheme}://open/{token}"))
    }

    pub(crate) fn parse_activation(scheme: &str, value: &str) -> Option<String> {
        if !scheme_valid(scheme) || value.len() > 180 {
            return None;
        }
        let parsed = url::Url::parse(value).ok()?;
        if parsed.scheme() != scheme
            || parsed.host_str() != Some("open")
            || !parsed.username().is_empty()
            || parsed.password().is_some()
            || parsed.port().is_some()
            || parsed.query().is_some()
            || parsed.fragment().is_some()
        {
            return None;
        }
        let token = parsed.path().strip_prefix('/')?;
        if value != format!("{scheme}://open/{token}") {
            return None;
        }
        token_valid(token).then(|| token.to_owned())
    }

    impl Store {
        pub(crate) fn open(root: PathBuf) -> Result<Self, String> {
            fs::create_dir_all(&root).map_err(|_| "无法保存提醒的对话入口")?;
            if !fs::symlink_metadata(&root)
                .map_err(|_| "无法检查提醒入口目录")?
                .file_type()
                .is_dir()
            {
                return Err("提醒入口目录不是本机普通目录".into());
            }
            Ok(Self {
                root,
                capacity: MAX_TICKETS,
            })
        }

        fn path(&self, token: &str) -> Result<PathBuf, String> {
            if !token_valid(token) {
                return Err("invalid notification link".into());
            }
            Ok(self.root.join(format!("{token}.json")))
        }

        fn read(&self, token: &str) -> Result<Ticket, String> {
            let path = self.path(token)?;
            let info = fs::symlink_metadata(&path).map_err(|_| "提醒的对话入口已失效")?;
            if !info.file_type().is_file() || info.len() > MAX_FILE {
                return Err("提醒的对话入口无效".into());
            }
            let mut raw = Vec::new();
            File::open(&path)
                .map_err(|_| "无法读取提醒的对话入口")?
                .take(MAX_FILE + 1)
                .read_to_end(&mut raw)
                .map_err(|_| "无法读取提醒的对话入口")?;
            if raw.len() as u64 > MAX_FILE {
                return Err("提醒的对话入口无效".into());
            }
            let ticket: Ticket = serde_json::from_slice(&raw).map_err(|_| "提醒的对话入口无效")?;
            if ticket.format != 1 || ticket.token != token || !ticket.route.valid() {
                return Err("提醒的对话入口无效".into());
            }
            Ok(ticket)
        }

        pub(crate) fn resolve(&self, now: u64, token: &str) -> Result<Ticket, String> {
            let ticket = self.read(token)?;
            if now < ticket.created_at || now.saturating_sub(ticket.created_at) >= TTL {
                return Err("提醒的对话入口已过期".into());
            }
            Ok(ticket)
        }

        // Only our validated immutable tickets are removed. Unknown files and
        // malformed partial writes remain untouched and never become links.
        pub(crate) fn create(
            &self,
            now: u64,
            notification_id: i32,
            route: Route,
        ) -> Result<(Ticket, Vec<Ticket>), String> {
            if !route.valid() || now == 0 {
                return Err("提醒缺少有效的对话入口".into());
            }
            let guard = OpenOptions::new()
                .create(true)
                .truncate(false)
                .read(true)
                .write(true)
                .open(self.root.join("routes.lock"))
                .map_err(|_| "无法核对提醒入口的写入状态")?;
            guard
                .try_lock()
                .map_err(|_| "提醒入口正在由另一个操作保存，请稍后重试")?;
            let mut retained = Vec::new();
            let mut retired = Vec::new();
            for (index, entry) in fs::read_dir(&self.root)
                .map_err(|_| "无法检查已保存的提醒入口")?
                .enumerate()
            {
                if index >= MAX_ENTRIES {
                    return Err("保存的提醒入口过多，请检查本机状态".into());
                }
                let entry = entry.map_err(|_| "无法检查已保存的提醒入口")?;
                let name = entry.file_name();
                let Some(token) = name.to_str().and_then(|name| name.strip_suffix(".json")) else {
                    continue;
                };
                if !token_valid(token) {
                    continue;
                }
                let Ok(ticket) = self.read(token) else {
                    continue;
                };
                if now.saturating_sub(ticket.created_at) >= TTL {
                    self.remove(&ticket.token)?;
                    retired.push(ticket);
                } else {
                    retained.push(ticket);
                }
            }
            retained.sort_by(|a, b| (a.created_at, &a.token).cmp(&(b.created_at, &b.token)));
            let excess = retained
                .len()
                .saturating_add(1)
                .saturating_sub(self.capacity);
            for ticket in retained.into_iter().take(excess) {
                self.remove(&ticket.token)?;
                retired.push(ticket);
            }
            let mut entropy = [0u8; 32];
            getrandom::fill(&mut entropy).map_err(|_| "无法创建随机提醒入口")?;
            let token: String = entropy.iter().map(|value| format!("{value:02x}")).collect();
            let ticket = Ticket {
                format: 1,
                token,
                created_at: now,
                notification_id,
                route,
            };
            let raw = serde_json::to_vec(&ticket).map_err(|_| "无法保存提醒的对话入口")?;
            let mut options = OpenOptions::new();
            options.write(true).create_new(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                options.mode(0o600);
            }
            let mut file = options
                .open(self.path(&ticket.token)?)
                .map_err(|_| "无法保存提醒的对话入口")?;
            file.write_all(&raw)
                .and_then(|_| file.sync_all())
                .map_err(|_| "无法保存提醒的对话入口")?;
            Ok((ticket, retired))
        }

        pub(crate) fn remove(&self, token: &str) -> Result<(), String> {
            self.read(token)?;
            fs::remove_file(self.path(token)?).map_err(|_| "无法清理本次提醒入口".into())
        }

        #[cfg(test)]
        fn test_root(&self) -> &std::path::Path {
            &self.root
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use serde_json::Value;

        struct TestRoot(PathBuf);
        impl TestRoot {
            fn new() -> Self {
                let mut bytes = [0u8; 16];
                getrandom::fill(&mut bytes).unwrap();
                let suffix: String = bytes.iter().map(|v| format!("{v:02x}")).collect();
                let path =
                    std::env::temp_dir().join(format!("mewdex-notification-routes-{suffix}"));
                fs::create_dir(&path).unwrap();
                Self(path)
            }
        }
        impl Drop for TestRoot {
            fn drop(&mut self) {
                let canonical = fs::canonicalize(&self.0).unwrap();
                let parent = fs::canonicalize(std::env::temp_dir()).unwrap();
                assert_eq!(canonical.parent(), Some(parent.as_path()));
                assert!(canonical
                    .file_name()
                    .unwrap()
                    .to_str()
                    .unwrap()
                    .starts_with("mewdex-notification-routes-"));
                fs::remove_dir_all(canonical).unwrap();
            }
        }
        fn route(session: &str) -> Route {
            Route {
                scope: "input scope".into(),
                device_id: "executor".into(),
                session_id: session.into(),
            }
        }

        #[test]
        fn native_link_survives_a_new_process_store_and_contains_only_navigation() {
            let root = TestRoot::new();
            let store = Store::open(root.0.clone()).unwrap();
            let (ticket, _) = store.create(100, 17, route("conversation")).unwrap();
            let uri = activation_url(SCHEME, &ticket.token).unwrap();
            assert_eq!(parse_activation(SCHEME, &uri), Some(ticket.token.clone()));
            assert!(!uri.contains("executor") && !uri.contains("conversation"));
            drop(store);
            let reopened = Store::open(root.0.clone()).unwrap();
            let read = reopened.resolve(101, &ticket.token).unwrap();
            assert_eq!(read.notification_id, 17);
            assert_eq!(read.route, route("conversation"));
            assert!(reopened.resolve(100 + TTL, &ticket.token).is_err());
            assert!(reopened.resolve(99, &ticket.token).is_err());
        }

        #[test]
        fn retention_retires_owned_tickets_and_preserves_foreign_files() {
            let root = TestRoot::new();
            let mut store = Store::open(root.0.clone()).unwrap();
            store.capacity = 2;
            let (first, _) = store.create(100, 1, route("first")).unwrap();
            let (second, _) = store.create(101, 2, route("second")).unwrap();
            fs::write(store.test_root().join("leave-user-file.txt"), "keep").unwrap();
            let corrupt = "a".repeat(64);
            fs::write(store.path(&corrupt).unwrap(), "partial write").unwrap();
            let (third, retired) = store.create(102, 3, route("third")).unwrap();
            assert_eq!(
                retired
                    .iter()
                    .map(|ticket| ticket.notification_id)
                    .collect::<Vec<_>>(),
                [1]
            );
            assert!(store.resolve(102, &first.token).is_err());
            assert!(store.resolve(102, &second.token).is_ok());
            assert!(store.resolve(102, &third.token).is_ok());
            let (_, retired) = store.create(102 + TTL, 4, route("later")).unwrap();
            assert_eq!(retired.len(), 2);
            assert_eq!(
                fs::read_to_string(store.test_root().join("leave-user-file.txt")).unwrap(),
                "keep"
            );
            assert_eq!(
                fs::read_to_string(store.path(&corrupt).unwrap()).unwrap(),
                "partial write"
            );
        }

        #[test]
        fn untrusted_links_metadata_and_files_do_not_become_routes() {
            let token = "0".repeat(64);
            for value in [
                format!("https://open/{token}"),
                format!("{SCHEME}://other/{token}"),
                format!("{SCHEME}://user@open/{token}"),
                format!("{SCHEME}://open:123/{token}"),
                format!("{SCHEME}://open/{token}?path=x"),
                format!("{SCHEME}://open/{token}#x"),
                format!("{SCHEME}://open/../{token}"),
                format!("{SCHEME}://open/%30{}", "0".repeat(63)),
            ] {
                assert!(parse_activation(SCHEME, &value).is_none(), "{value}");
            }
            let root = TestRoot::new();
            let store = Store::open(root.0.clone()).unwrap();
            assert!(store.resolve(100, "../outside").is_err());
            let (ticket, _) = store.create(100, 1, route("valid")).unwrap();
            let mut raw: Value =
                serde_json::from_slice(&fs::read(store.path(&ticket.token).unwrap()).unwrap())
                    .unwrap();
            raw["route"]["sessionId"] = Value::String("x".repeat(513));
            fs::write(
                store.path(&ticket.token).unwrap(),
                serde_json::to_vec(&raw).unwrap(),
            )
            .unwrap();
            assert!(store.resolve(100, &ticket.token).is_err());
            fs::write(
                store.path(&ticket.token).unwrap(),
                vec![b'x'; MAX_FILE as usize + 1],
            )
            .unwrap();
            assert!(store.resolve(100, &ticket.token).is_err());
        }
    }
}
