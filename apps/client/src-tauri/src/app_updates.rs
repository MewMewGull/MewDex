//! Release metadata and packages come from the bundled repository. The webview
//! selects a channel and confirms a checked version, never an executable URL.
use quick_xml::{events::Event, Reader};
use reqwest::{Client, StatusCode};
use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    path::{Path, PathBuf},
    sync::atomic::{AtomicBool, Ordering},
    time::{Duration, Instant},
};
use tauri::Manager;
use tokio::{io::AsyncWriteExt, sync::Mutex};

const REPOSITORY: &str = "MewMewGull/MewDex";
const MAX_METADATA: u64 = 2 * 1024 * 1024;
const MAX_PACKAGE: u64 = 2 * 1024 * 1024 * 1024;

#[derive(Default)]
pub(crate) struct Updater {
    pending: Mutex<Pending>,
    cancelled: AtomicBool,
}

#[derive(Default)]
struct Pending {
    release: Option<Release>,
    staged: Option<PathBuf>,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Info {
    current_version: String,
    platform: String,
    arch: String,
    can_download: bool,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Release {
    tag: String,
    version: String,
    notes: String,
    url: String,
    published_at: Option<String>,
    prerelease: bool,
    package: Option<Package>,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Package {
    name: String,
    bytes: u64,
    sha256: String,
    #[serde(default)]
    update_mode: String,
    #[serde(default)]
    installation: String,
}

#[derive(Default, Deserialize)]
struct GitHubRelease {
    tag_name: String,
    #[serde(default)]
    body: Option<String>,
    #[serde(default)]
    published_at: Option<String>,
    #[serde(default)]
    draft: bool,
    #[serde(default)]
    prerelease: bool,
    #[serde(skip)]
    from_feed: bool,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Manifest {
    schema_version: u32,
    tag: String,
    prerelease: bool,
    #[serde(default)]
    summary: String,
    assets: Vec<Asset>,
}

#[derive(Deserialize)]
struct Asset {
    os: String,
    arch: String,
    kind: String,
    #[serde(flatten)]
    package: Package,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct Progress {
    downloaded: u64,
    total: u64,
}

fn version(tag: &str) -> Option<Version> {
    Version::parse(tag.strip_prefix('v').unwrap_or(tag)).ok()
}

fn platform() -> &'static str {
    match std::env::consts::OS {
        "macos" => "macos",
        other => other,
    }
}

fn architecture() -> &'static str {
    match std::env::consts::ARCH {
        "x86_64" => "x64",
        "aarch64" => "arm64",
        other => other,
    }
}

#[tauri::command]
pub(crate) fn app_update_info(app: tauri::AppHandle) -> Info {
    Info {
        current_version: app.package_info().version.to_string(),
        platform: platform().into(),
        arch: architecture().into(),
        can_download: cfg!(desktop),
    }
}

fn github_host(host: &str) -> bool {
    matches!(
        host,
        "github.com"
            | "api.github.com"
            | "release-assets.githubusercontent.com"
            | "objects.githubusercontent.com"
            | "github-releases.githubusercontent.com"
    )
}

fn client() -> Result<Client, String> {
    // Bundled roots keep release checks independent of Android JVM verifier
    // initialization and use the same HTTPS validation on every platform.
    let tls = rustls::ClientConfig::builder()
        .with_root_certificates(rustls::RootCertStore {
            roots: webpki_roots::TLS_SERVER_ROOTS.to_vec(),
        })
        .with_no_client_auth();
    Client::builder()
        .tls_backend_preconfigured(tls)
        .user_agent(concat!("MewDex/", env!("CARGO_PKG_VERSION")))
        .https_only(true)
        .connect_timeout(Duration::from_secs(10))
        .read_timeout(Duration::from_secs(30))
        .timeout(Duration::from_secs(45))
        .redirect(reqwest::redirect::Policy::custom(|attempt| {
            if attempt.previous().len() < 5
                && attempt.url().scheme() == "https"
                && attempt.url().host_str().is_some_and(github_host)
            {
                attempt.follow()
            } else {
                attempt.error("Unexpected GitHub release redirect")
            }
        }))
        .build()
        .map_err(|_| "无法初始化 GitHub 更新连接".into())
}

fn asset_url(tag: &str, name: &str) -> Result<reqwest::Url, String> {
    let mut url = reqwest::Url::parse(&format!(
        "https://github.com/{REPOSITORY}/releases/download/"
    ))
    .map_err(|_| "无法构建发布地址")?;
    url.path_segments_mut()
        .map_err(|_| "无法构建发布地址")?
        .pop_if_empty()
        .push(tag)
        .push(name);
    Ok(url)
}

async fn metadata(mut response: reqwest::Response) -> Result<Vec<u8>, String> {
    let mut bytes = Vec::new();
    while let Some(chunk) = response
        .chunk()
        .await
        .map_err(|_| "读取 GitHub 更新信息失败")?
    {
        if bytes.len() as u64 + chunk.len() as u64 > MAX_METADATA {
            return Err("GitHub 更新信息超过大小限制".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    Ok(bytes)
}

fn atom_releases(bytes: &[u8]) -> Result<Vec<GitHubRelease>, String> {
    let mut reader = Reader::from_reader(bytes);
    let mut releases = Vec::new();
    let prefix = format!("https://github.com/{REPOSITORY}/releases/tag/");
    loop {
        match reader.read_event().map_err(|_| "GitHub 发布订阅格式无效")? {
            Event::Empty(element) | Event::Start(element) if element.name().as_ref() == "link" => {
                for attribute in element.attributes() {
                    let attribute = attribute.map_err(|_| "GitHub 发布链接无效")?;
                    if attribute.key.as_ref() != "href" {
                        continue;
                    }
                    let value = attribute
                        .normalized_value(quick_xml::XmlVersion::Implicit1_0)
                        .map_err(|_| "GitHub 发布链接无效")?;
                    if let Some(tag) = value.strip_prefix(&prefix) {
                        // Supported SemVer tags are ASCII. GitHub escapes build
                        // metadata's plus sign in some feed representations.
                        let tag = tag.replace("%2B", "+").replace("%2b", "+");
                        if let Some(parsed) = version(&tag) {
                            releases.push(GitHubRelease {
                                tag_name: tag,
                                prerelease: !parsed.pre.is_empty(),
                                from_feed: true,
                                ..Default::default()
                            });
                        }
                    }
                }
            }
            Event::Eof => return Ok(releases),
            _ => (),
        }
    }
}

async fn releases(client: &Client) -> Result<Vec<GitHubRelease>, String> {
    let api = client
        .get(format!(
            "https://api.github.com/repos/{REPOSITORY}/releases?per_page=30"
        ))
        .header("Accept", "application/vnd.github+json")
        .send()
        .await;
    if let Ok(response) = api {
        if response.status().is_success() {
            return serde_json::from_slice(&metadata(response).await?)
                .map_err(|_| "GitHub Release 数据格式无效".into());
        }
    }
    // This endpoint also works when anonymous API requests share a rate limit.
    let response = client
        .get(format!("https://github.com/{REPOSITORY}/releases.atom"))
        .send()
        .await
        .map_err(|_| "无法连接 GitHub，请检查网络后重试")?;
    if !response.status().is_success() {
        return Err(format!(
            "GitHub 更新检测失败（HTTP {}）",
            response.status().as_u16()
        ));
    }
    atom_releases(&metadata(response).await?)
}

fn package_rank(asset: &Asset, os: &str, arch: &str) -> Option<usize> {
    if asset.os != os || asset.arch != arch || asset.kind != "installer" {
        return None;
    }
    let extensions: &[&str] = match os {
        "windows" => &[".exe", ".msi"],
        "macos" => &[".dmg", ".pkg"],
        "linux" => &[".appimage", ".deb", ".rpm"],
        "android" => &[".apk"],
        "ios" => &[".ipa"],
        _ => return None,
    };
    let package = &asset.package;
    if package.name.is_empty()
        || package
            .name
            .chars()
            .any(|ch| ch.is_control() || "/\\:\"<>|?*".contains(ch))
        || package.bytes == 0
        || package.bytes > MAX_PACKAGE
        || package.sha256.len() != 64
        || !package.sha256.bytes().all(|byte| byte.is_ascii_hexdigit())
    {
        return None;
    }
    let name = package.name.to_ascii_lowercase();
    extensions
        .iter()
        .position(|extension| name.ends_with(extension))
}

fn select_package(
    manifest: Manifest,
    tag: &str,
    os: &str,
    arch: &str,
) -> Result<Option<Package>, String> {
    if manifest.schema_version != 1 || manifest.tag != tag {
        return Err("发布清单与 GitHub Release 版本不一致".into());
    }
    Ok(manifest
        .assets
        .into_iter()
        .filter_map(|asset| package_rank(&asset, os, arch).map(|rank| (rank, asset.package)))
        .min_by_key(|(rank, _)| *rank)
        .map(|(_, package)| package))
}

fn newer_releases(
    releases: Vec<GitHubRelease>,
    current: &Version,
    channel: &str,
) -> Vec<(Version, GitHubRelease)> {
    let mut candidates: Vec<_> = releases
        .into_iter()
        .filter_map(|release| {
            let parsed = version(&release.tag_name)?;
            let preview = release.prerelease || !parsed.pre.is_empty();
            (!release.draft
                && parsed.cmp_precedence(current).is_gt()
                && (channel == "preview" || !preview))
                .then_some((parsed, release))
        })
        .collect();
    candidates.sort_by(|a, b| b.0.cmp_precedence(&a.0));
    candidates
}

async fn check(
    client: &Client,
    current: &Version,
    channel: &str,
    os: &str,
    arch: &str,
) -> Result<Option<Release>, String> {
    for (parsed, release) in newer_releases(releases(client).await?, current, channel) {
        let response = client
            .get(asset_url(&release.tag_name, "release-manifest.json")?)
            .send()
            .await
            .map_err(|_| "无法读取发布清单，请重试")?;
        let mut notes = release.body.unwrap_or_default();
        let mut package = None;
        let mut prerelease = release.prerelease || !parsed.pre.is_empty();
        if response.status().is_success() {
            let manifest: Manifest = serde_json::from_slice(&metadata(response).await?)
                .map_err(|_| "发布清单格式无效")?;
            prerelease |= manifest.prerelease;
            if channel == "stable" && prerelease {
                continue;
            }
            if notes.is_empty() {
                notes = manifest.summary.clone();
            }
            package = select_package(manifest, &release.tag_name, os, arch)?;
        } else if response.status() == StatusCode::NOT_FOUND
            && release.from_feed
            && channel == "stable"
        {
            return Err("发布缺少清单，暂时无法确认稳定版更新，请在 GitHub 查看发布状态".into());
        } else if response.status() != StatusCode::NOT_FOUND {
            return Err(format!(
                "读取发布清单失败（HTTP {}）",
                response.status().as_u16()
            ));
        }
        return Ok(Some(Release {
            url: format!(
                "https://github.com/{REPOSITORY}/releases/tag/{}",
                release.tag_name
            ),
            tag: release.tag_name,
            version: parsed.to_string(),
            notes,
            published_at: release.published_at,
            prerelease,
            package,
        }));
    }
    Ok(None)
}

#[tauri::command]
pub(crate) async fn app_update_check(
    app: tauri::AppHandle,
    updater: tauri::State<'_, Updater>,
    channel: String,
) -> Result<Option<Release>, String> {
    if !matches!(channel.as_str(), "stable" | "preview") {
        return Err("未知的更新通道".into());
    }
    let mut pending = updater.pending.try_lock().map_err(|_| "更新操作正在进行")?;
    let release = check(
        &client()?,
        &app.package_info().version,
        &channel,
        platform(),
        architecture(),
    )
    .await?;
    *pending = Pending {
        release: release.clone(),
        staged: None,
    };
    Ok(release)
}

#[cfg(desktop)]
async fn verify(path: &Path, package: &Package) -> Result<bool, String> {
    use tokio::io::AsyncReadExt;
    let mut file = match tokio::fs::File::open(path).await {
        Ok(file) => file,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(false),
        Err(_) => return Err("无法读取已下载的更新包".into()),
    };
    if file
        .metadata()
        .await
        .map_err(|_| "无法读取更新包大小")?
        .len()
        != package.bytes
    {
        return Ok(false);
    }
    let mut digest = Sha256::new();
    let mut buffer = vec![0u8; 128 * 1024];
    loop {
        let length = file.read(&mut buffer).await.map_err(|_| "无法校验更新包")?;
        if length == 0 {
            return Ok(format!("{:x}", digest.finalize()).eq_ignore_ascii_case(&package.sha256));
        }
        digest.update(&buffer[..length]);
    }
}

#[cfg(desktop)]
#[tauri::command]
pub(crate) async fn app_update_download(
    app: tauri::AppHandle,
    updater: tauri::State<'_, Updater>,
    tag: String,
    on_progress: tauri::ipc::Channel<Progress>,
) -> Result<(), String> {
    let mut pending = updater.pending.try_lock().map_err(|_| "更新操作正在进行")?;
    let release = pending
        .release
        .as_ref()
        .filter(|release| release.tag == tag)
        .ok_or("请重新检查更新后再下载")?;
    let package = release
        .package
        .clone()
        .ok_or("此版本尚未提供适合本机的更新包")?;
    pending.staged = None;
    updater.cancelled.store(false, Ordering::Relaxed);
    let directory = app
        .path()
        .app_cache_dir()
        .map_err(|_| "无法定位更新缓存目录")?
        .join("updates")
        .join(package.sha256.to_ascii_lowercase());
    tokio::fs::create_dir_all(&directory)
        .await
        .map_err(|_| "无法创建更新缓存目录")?;
    let extension = Path::new(&package.name)
        .extension()
        .ok_or("更新包类型无效")?;
    let target = directory.join(Path::new("MewDex-update").with_extension(extension));
    if verify(&target, &package).await? {
        pending.staged = Some(target);
        let _ = on_progress.send(Progress {
            downloaded: package.bytes,
            total: package.bytes,
        });
        return Ok(());
    }
    let temporary = target.with_extension("part");
    let result: Result<(), String> = async {
        let mut response = client()?
            .get(asset_url(&tag, &package.name)?)
            .timeout(Duration::from_secs(30 * 60))
            .send()
            .await
            .map_err(|_| "无法下载更新包，请检查网络后重试")?;
        if !response.status().is_success() {
            return Err(format!(
                "更新包下载失败（HTTP {}）",
                response.status().as_u16()
            ));
        }
        let mut file = tokio::fs::File::create(&temporary)
            .await
            .map_err(|_| "无法写入更新缓存")?;
        let mut downloaded = 0u64;
        let mut digest = Sha256::new();
        let mut notified = Instant::now();
        while let Some(chunk) = response
            .chunk()
            .await
            .map_err(|_| "更新包下载中断，请重试")?
        {
            if updater.cancelled.load(Ordering::Relaxed) {
                return Err("更新下载已取消".into());
            }
            downloaded += chunk.len() as u64;
            if downloaded > package.bytes {
                return Err("更新包大小与发布清单不一致".into());
            }
            file.write_all(&chunk)
                .await
                .map_err(|_| "无法保存更新包，请检查磁盘空间")?;
            digest.update(&chunk);
            if notified.elapsed() >= Duration::from_millis(150) {
                let _ = on_progress.send(Progress {
                    downloaded,
                    total: package.bytes,
                });
                notified = Instant::now();
            }
        }
        if updater.cancelled.load(Ordering::Relaxed) {
            return Err("更新下载已取消".into());
        }
        if downloaded != package.bytes
            || !format!("{:x}", digest.finalize()).eq_ignore_ascii_case(&package.sha256)
        {
            return Err("更新包 SHA-256 校验失败，请重新下载".into());
        }
        file.sync_all().await.map_err(|_| "无法完成更新包保存")?;
        drop(file);
        if target.exists() {
            tokio::fs::remove_file(&target)
                .await
                .map_err(|_| "无法替换旧的更新缓存")?;
        }
        tokio::fs::rename(&temporary, &target)
            .await
            .map_err(|_| "无法完成更新包保存")?;
        let _ = on_progress.send(Progress {
            downloaded,
            total: package.bytes,
        });
        Ok(())
    }
    .await;
    if result.is_err() {
        let _ = tokio::fs::remove_file(&temporary).await;
    } else {
        pending.staged = Some(target);
    }
    result
}

#[cfg(desktop)]
#[tauri::command]
pub(crate) fn app_update_cancel(updater: tauri::State<'_, Updater>) {
    updater.cancelled.store(true, Ordering::Relaxed);
}

#[cfg(desktop)]
#[tauri::command]
pub(crate) async fn app_update_install(
    app: tauri::AppHandle,
    updater: tauri::State<'_, Updater>,
    tag: String,
) -> Result<(), String> {
    let pending = updater.pending.try_lock().map_err(|_| "更新操作正在进行")?;
    let release = pending
        .release
        .as_ref()
        .filter(|release| release.tag == tag)
        .ok_or("请重新检查更新")?;
    let package = release.package.as_ref().ok_or("没有可安装的更新包")?;
    let path = pending.staged.as_ref().ok_or("请先下载更新包")?;
    if !verify(path, package).await? {
        return Err("更新包已变化，请重新下载".into());
    }
    #[cfg(target_os = "windows")]
    if package.update_mode == "windows-nsis" && package.name.to_ascii_lowercase().ends_with(".exe")
    {
        use std::{
            os::windows::process::CommandExt,
            process::{Command, Stdio},
        };
        let current = std::env::current_exe().map_err(|_| "无法定位当前安装目录")?;
        let directory = current.parent().ok_or("无法定位当前安装目录")?;
        Command::new(path)
            .arg(format!("/UPDATEPID={}", std::process::id()))
            // NSIS requires /D to be last and unquoted, including paths with spaces.
            .raw_arg(format!("/D={}", directory.display()))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| "无法启动更新安装程序")?;
        app.exit(0);
        return Ok(());
    }
    use tauri_plugin_opener::OpenerExt;
    if package.name.to_ascii_lowercase().ends_with(".appimage") {
        app.opener()
            .reveal_item_in_dir(path)
            .map_err(|_| "无法打开更新包所在目录")?;
    } else {
        app.opener()
            .open_path(path.to_string_lossy(), None::<&str>)
            .map_err(|_| "无法打开系统安装程序")?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn manifest(assets: serde_json::Value) -> Manifest {
        serde_json::from_value(json!({
            "schemaVersion": 1, "tag": "v1.2.0", "prerelease": false, "assets": assets
        }))
        .unwrap()
    }

    fn asset(os: &str, arch: &str, name: &str) -> serde_json::Value {
        json!({
            "os": os, "arch": arch, "kind": "installer",
            "name": name, "bytes": 10, "sha256": "a".repeat(64)
        })
    }

    #[test]
    fn channels_compare_semver_and_ignore_drafts_downgrades_and_build_metadata() {
        let tags = [
            "v1.0.0+new",
            "v0.9.0",
            "v1.10.0",
            "v1.9.0",
            "v2.0.0-preview.1",
            "invalid",
        ];
        let releases = || {
            tags.iter()
                .map(|tag| GitHubRelease {
                    tag_name: (*tag).into(),
                    ..Default::default()
                })
                .chain([
                    GitHubRelease {
                        tag_name: "v9.0.0".into(),
                        draft: true,
                        ..Default::default()
                    },
                    GitHubRelease {
                        tag_name: "v8.0.0".into(),
                        prerelease: true,
                        ..Default::default()
                    },
                ])
                .collect()
        };
        let stable = newer_releases(releases(), &version("1.0.0").unwrap(), "stable");
        assert_eq!(
            stable
                .iter()
                .map(|(_, release)| release.tag_name.as_str())
                .collect::<Vec<_>>(),
            ["v1.10.0", "v1.9.0"]
        );
        let preview = newer_releases(releases(), &version("1.0.0").unwrap(), "preview");
        assert_eq!(preview[0].1.tag_name, "v8.0.0");
        assert_eq!(preview[1].1.tag_name, "v2.0.0-preview.1");
        assert_eq!(
            newer_releases(
                vec![GitHubRelease {
                    tag_name: "v1.0.0".into(),
                    ..Default::default()
                }],
                &version("1.0.0-preview.2").unwrap(),
                "stable"
            )
            .len(),
            1
        );
    }

    #[test]
    fn atom_fallback_accepts_only_this_repositorys_version_links() {
        let feed = br#"<feed xmlns="http://www.w3.org/2005/Atom">
          <link href="https://github.com/MewMewGull/MewDex/releases"/>
          <entry><link href="https://github.com/MewMewGull/MewDex/releases/tag/v1.2.0"/></entry>
          <entry><link href="https://github.com/MewMewGull/MewDex/releases/tag/v1.3.0-preview.1%2Bbuild"/></entry>
          <entry><link href="https://github.com/other/repo/releases/tag/v99.0.0"/></entry>
          <entry><link href="https://github.com/MewMewGull/MewDex/releases/tag/not-a-version"/></entry>
        </feed>"#;
        let releases = atom_releases(feed).unwrap();
        assert_eq!(releases.len(), 2);
        assert_eq!(releases[1].tag_name, "v1.3.0-preview.1+build");
        assert!(releases[1].prerelease);
        assert!(atom_releases(b"<feed><link").is_err());
    }

    #[test]
    fn packages_match_os_architecture_and_prefer_native_installers() {
        let assets = json!([
            asset("windows", "arm64", "MewDex-arm64.exe"),
            asset("windows", "x64", "MewDex.msi"),
            asset("windows", "x64", "MewDex.exe"),
            asset("macos", "arm64", "MewDex.dmg"),
            asset("android", "arm64", "MewDex.apk"),
        ]);
        let selected = select_package(manifest(assets.clone()), "v1.2.0", "windows", "x64")
            .unwrap()
            .unwrap();
        assert_eq!(selected.name, "MewDex.exe");
        assert!(selected.update_mode.is_empty()); // Older manifests use the system installer.
        assert_eq!(
            select_package(manifest(assets.clone()), "v1.2.0", "macos", "arm64")
                .unwrap()
                .unwrap()
                .name,
            "MewDex.dmg"
        );
        assert!(
            select_package(manifest(assets.clone()), "v1.2.0", "linux", "x64")
                .unwrap()
                .is_none()
        );
        assert!(select_package(manifest(assets), "v9.0.0", "windows", "x64").is_err());
    }

    #[test]
    fn malformed_and_unbounded_assets_are_never_selected() {
        for (field, value) in [
            ("name", json!("../MewDex.exe")),
            ("name", json!("MewDex.exe:payload")),
            ("bytes", json!(0)),
            ("bytes", json!(MAX_PACKAGE + 1)),
            ("sha256", json!("a".repeat(63))),
            ("sha256", json!("z".repeat(64))),
            ("kind", json!("portable")),
        ] {
            let mut value_asset = asset("windows", "x64", "MewDex.exe");
            value_asset[field] = value;
            assert!(
                select_package(manifest(json!([value_asset])), "v1.2.0", "windows", "x64")
                    .unwrap()
                    .is_none()
            );
        }
        assert_eq!(
            asset_url("v1.2.0+build", "MewDex x64.exe")
                .unwrap()
                .as_str(),
            "https://github.com/MewMewGull/MewDex/releases/download/v1.2.0+build/MewDex%20x64.exe"
        );
        assert!(github_host("release-assets.githubusercontent.com"));
        assert!(!github_host("github.com.attacker.invalid"));
    }

    #[cfg(desktop)]
    #[test]
    fn downloaded_packages_are_reverified_and_corruption_is_rejected() {
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        runtime.block_on(async {
            let directory =
                std::env::temp_dir().join(format!("mewdex-update-test-{}", std::process::id()));
            tokio::fs::create_dir_all(&directory).await.unwrap();
            let path = directory.join("package.exe");
            let bytes = b"owned update fixture";
            let package = Package {
                name: "MewDex.exe".into(),
                bytes: bytes.len() as u64,
                sha256: format!("{:x}", Sha256::digest(bytes)),
                update_mode: String::new(),
                installation: String::new(),
            };
            tokio::fs::write(&path, bytes).await.unwrap();
            assert!(verify(&path, &package).await.unwrap());
            tokio::fs::write(&path, b"owned corruptfixture")
                .await
                .unwrap();
            assert!(!verify(&path, &package).await.unwrap());
            tokio::fs::remove_file(&path).await.unwrap();
            assert!(!verify(&path, &package).await.unwrap());
            tokio::fs::remove_dir(directory).await.unwrap();
        });
    }

    #[test]
    #[ignore = "uses the public GitHub release endpoints"]
    fn live_release_check() {
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        runtime.block_on(async {
            let client = client().unwrap();
            let releases = releases(&client).await.unwrap();
            println!("GitHub release source returned {} entries", releases.len());
            let release = check(
                &client,
                &version("0.0.0").unwrap(),
                "preview",
                platform(),
                architecture(),
            )
            .await
            .unwrap();
            println!(
                "Latest compatible release metadata {}",
                serde_json::to_string(&release).unwrap()
            );
        });
    }
}
