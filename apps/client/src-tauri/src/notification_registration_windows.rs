//! Per-user notification identity and protocol registration. No machine-wide
//! settings or existing foreign associations are replaced. Registration is an
//! explicit enable operation, never part of permission inspection or startup.
use std::{fs, path::PathBuf};
use windows::{
    core::{Interface, BSTR, GUID, HSTRING},
    Win32::{
        Foundation::{PROPERTYKEY, RPC_E_CHANGED_MODE},
        System::Com::{
            CoCreateInstance, CoInitializeEx, CoTaskMemFree, CoUninitialize, IPersistFile,
            StructuredStorage::{InitPropVariantFromCLSID, PropVariantToGUID, PROPVARIANT},
            CLSCTX_INPROC_SERVER, COINIT_MULTITHREADED, STGM_READ, STGM_READWRITE,
        },
        UI::Shell::{
            FOLDERID_Programs, IShellLinkW, PropertiesSystem::IPropertyStore, SHGetKnownFolderPath,
            ShellLink, KF_FLAG_DEFAULT,
        },
    },
};
use winreg::{
    enums::{HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE},
    RegKey,
};

const AUMID: PROPERTYKEY = PROPERTYKEY {
    fmtid: GUID::from_u128(0x9f4c2855_9f79_4b39_a8d0_e1d42de1d5f3),
    pid: 5,
};
const ACTIVATOR: PROPERTYKEY = PROPERTYKEY {
    fmtid: AUMID.fmtid,
    pid: 26,
};
const STUB: GUID = GUID::from_u128(0x37cb0ed3_5a0b_4a5d_9218_9b661bb73bd3);

pub(crate) struct Apartment(bool);
impl Apartment {
    pub(crate) fn enter() -> Result<Self, String> {
        let result = unsafe { CoInitializeEx(None, COINIT_MULTITHREADED) };
        if result.is_ok() {
            Ok(Self(true))
        } else if result == RPC_E_CHANGED_MODE {
            Ok(Self(false))
        } else {
            Err("系统提醒的本机接口暂时不可用".into())
        }
    }
}
impl Drop for Apartment {
    fn drop(&mut self) {
        if self.0 {
            unsafe { CoUninitialize() }
        }
    }
}

pub(crate) struct Registration {
    pub(crate) app_id: String,
    pub(crate) scheme: String,
    pub(crate) program: PathBuf,
    shortcut: PathBuf,
    title: String,
}

fn valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 128
        && id
            .bytes()
            .all(|v| v.is_ascii_alphanumeric() || b".-".contains(&v))
}

impl Registration {
    pub(crate) fn new(
        app_id: String,
        scheme: String,
        title: String,
        program: PathBuf,
    ) -> Result<Self, String> {
        if !valid_id(&app_id)
            || !crate::notification_routes::tickets::scheme_valid(&scheme)
            || !program.is_absolute()
            || !program.is_file()
            || title.is_empty()
            || title.len() > 128
            || title.contains(['/', '\\', '\0'])
        {
            return Err("系统提醒的应用标识无效".into());
        }
        let folder = unsafe { SHGetKnownFolderPath(&FOLDERID_Programs, KF_FLAG_DEFAULT, None) }
            .map_err(|_| "无法定位当前用户的应用快捷方式")?;
        let directory = unsafe { folder.to_string() };
        unsafe { CoTaskMemFree(Some(folder.0.cast())) };
        let shortcut = PathBuf::from(directory.map_err(|_| "应用快捷方式路径无效")?)
            .join(format!("{title}.lnk"));
        Ok(Self {
            app_id,
            scheme,
            title,
            program,
            shortcut,
        })
    }

    pub(crate) fn for_app(app: &tauri::AppHandle) -> Result<Self, String> {
        Self::new(
            app.config().identifier.clone(),
            super::notification_routes::tickets::SCHEME.into(),
            app.config()
                .product_name
                .clone()
                .unwrap_or_else(|| "MewDex".into()),
            std::env::current_exe().map_err(|_| "无法定位本机应用程序")?,
        )
    }

    fn app_key(&self) -> String {
        format!("Software\\Classes\\AppUserModelId\\{}", self.app_id)
    }
    fn protocol_key(&self) -> String {
        format!("Software\\Classes\\{}", self.scheme)
    }
    fn program_text(&self) -> Result<&str, String> {
        self.program
            .to_str()
            .ok_or_else(|| "本机应用程序路径不能用于系统提醒".into())
    }
    fn command(&self) -> Result<String, String> {
        Ok(format!("\"{}\" \"%1\"", self.program_text()?))
    }

    fn key_owned(&self, key: &RegKey) -> bool {
        let Ok(program) = self.program_text() else {
            return false;
        };
        matches!(key.get_value::<String, _>("MewDexOwner"), Ok(value) if value == self.app_id)
            && matches!(key.get_value::<String, _>("MewDexProgram"), Ok(value) if value == program)
    }

    fn preflight_key(&self, path: &str) -> Result<(), String> {
        match RegKey::predef(HKEY_LOCAL_MACHINE).open_subkey(path) {
            Ok(key) if self.key_owned(&key) => (),
            Ok(_) => return Err("此提醒标识已由系统中的其他安装使用，原关联已保留".into()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
            Err(_) => return Err("无法读取已有的系统提醒关联".into()),
        }
        match RegKey::predef(HKEY_CURRENT_USER).open_subkey(path) {
            Ok(key) if self.key_owned(&key) => Ok(()),
            Ok(_) => Err("此提醒标识已由其他安装使用，原关联已保留".into()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(_) => Err("无法核对当前用户的提醒关联".into()),
        }
    }

    fn protocol_plain(&self, root: &RegKey) -> Result<bool, String> {
        // DelegateExecute, DDE or another verb can redirect an otherwise
        // unchanged default command. Preserve such changes for explicit review.
        for (path, values, children) in [
            (
                "",
                &["", "URL Protocol", "MewDexOwner", "MewDexProgram"][..],
                &["shell"][..],
            ),
            ("shell", &[][..], &["open"][..]),
            ("shell\\open", &[][..], &["command"][..]),
            ("shell\\open\\command", &[""][..], &[][..]),
        ] {
            let node = match root.open_subkey(path) {
                Ok(node) => node,
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => continue,
                Err(_) => return Err("无法核对提醒的启动关联".into()),
            };
            for value in node.enum_values().take(9) {
                if !values.contains(&value.map_err(|_| "无法核对提醒的启动关联")?.0.as_str())
                {
                    return Ok(false);
                }
            }
            for child in node.enum_keys().take(9) {
                if !children.contains(&child.map_err(|_| "无法核对提醒的启动关联")?.as_str())
                {
                    return Ok(false);
                }
            }
        }
        Ok(true)
    }

    fn user_choice_owned(&self) -> Result<bool, String> {
        let path = format!(
            "Software\\Microsoft\\Windows\\Shell\\Associations\\UrlAssociations\\{}\\UserChoice",
            self.scheme
        );
        match RegKey::predef(HKEY_CURRENT_USER).open_subkey(path) {
            Ok(key) => Ok(
                matches!(key.get_value::<String, _>("ProgId"), Ok(value) if value == self.scheme),
            ),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(true),
            Err(_) => Err("无法核对提醒链接的默认应用".into()),
        }
    }

    fn load_link(&self, write: bool) -> Result<IShellLinkW, String> {
        let link: IShellLinkW = unsafe { CoCreateInstance(&ShellLink, None, CLSCTX_INPROC_SERVER) }
            .map_err(|_| "无法读取应用快捷方式")?;
        let persist: IPersistFile = link.cast().map_err(|_| "无法读取应用快捷方式")?;
        unsafe {
            persist.Load(
                &HSTRING::from(self.shortcut.to_str().ok_or("快捷方式路径无效")?),
                if write { STGM_READWRITE } else { STGM_READ },
            )
        }
        .map_err(|_| "无法读取应用快捷方式")?;
        Ok(link)
    }

    fn link_owned(&self, link: &IShellLinkW) -> Result<bool, String> {
        let mut path = vec![0u16; 32768];
        unsafe { link.GetPath(&mut path, std::ptr::null_mut(), 4) }
            .map_err(|_| "无法核对应用快捷方式")?;
        let path =
            String::from_utf16(&path[..path.iter().position(|v| *v == 0).unwrap_or(path.len())])
                .map_err(|_| "快捷方式目标无效")?;
        let props: IPropertyStore = link.cast().map_err(|_| "无法核对快捷方式标识")?;
        let identity = unsafe { props.GetValue(&AUMID) }.map_err(|_| "无法核对快捷方式标识")?;
        let identity = BSTR::try_from(&identity)
            .map_err(|_| "快捷方式缺少应用标识")?
            .to_string();
        let expected = fs::canonicalize(&self.program).map_err(|_| "无法核对本机应用程序")?;
        Ok(fs::canonicalize(path).ok() == Some(expected) && identity == self.app_id)
    }

    pub(crate) fn ready(&self) -> Result<bool, String> {
        let _apartment = Apartment::enter()?;
        let user = RegKey::predef(HKEY_CURRENT_USER);
        let Ok(app) = user.open_subkey(self.app_key()) else {
            return Ok(false);
        };
        let Ok(protocol) = user.open_subkey(self.protocol_key()) else {
            return Ok(false);
        };
        if !self.key_owned(&app) || !self.key_owned(&protocol) {
            return Ok(false);
        }
        if !self.protocol_plain(&protocol)? || !self.user_choice_owned()? {
            return Ok(false);
        }
        if !matches!(protocol.get_value::<String, _>("URL Protocol"), Ok(value) if value.is_empty())
        {
            return Ok(false);
        }
        let command: String = protocol
            .open_subkey("shell\\open\\command")
            .and_then(|key| key.get_value(""))
            .unwrap_or_default();
        if command != self.command()? {
            return Ok(false);
        }
        let Ok(info) = fs::symlink_metadata(&self.shortcut) else {
            return Ok(false);
        };
        if !info.file_type().is_file() {
            return Ok(false);
        }
        let link = self.load_link(false)?;
        if !self.link_owned(&link)? {
            return Ok(false);
        }
        let props: IPropertyStore = link.cast().map_err(|_| "无法核对快捷方式标识")?;
        let activator =
            unsafe { props.GetValue(&ACTIVATOR) }.map_err(|_| "无法核对提醒入口标识")?;
        Ok(unsafe { PropVariantToGUID(&activator) }.ok() == Some(STUB))
    }

    pub(crate) fn install(&self) -> Result<(), String> {
        let _apartment = Apartment::enter()?;
        if self.ready()? {
            return Ok(());
        }
        self.preflight_key(&self.app_key())?;
        self.preflight_key(&self.protocol_key())?;
        if !self.user_choice_owned()? {
            return Err("提醒链接已选择其他默认应用，原关联已保留".into());
        }
        if let Ok(protocol) = RegKey::predef(HKEY_CURRENT_USER).open_subkey(self.protocol_key()) {
            if !self.protocol_plain(&protocol)? {
                return Err("提醒的启动关联已变化，原配置已保留".into());
            }
            match protocol
                .open_subkey("shell\\open\\command")
                .and_then(|key| key.get_value::<String, _>(""))
            {
                Ok(command) if command != self.command()? => {
                    return Err("提醒的启动关联已变化，原配置已保留".into())
                }
                Err(error) if error.kind() != std::io::ErrorKind::NotFound => {
                    return Err("无法核对提醒的启动关联".into())
                }
                _ => (),
            }
        }
        let link: IShellLinkW = if let Ok(info) = fs::symlink_metadata(&self.shortcut) {
            if !info.file_type().is_file() {
                return Err("已有快捷方式不是普通文件，原文件已保留".into());
            }
            let link = self.load_link(true)?;
            if !self.link_owned(&link)? {
                return Err("已有同名快捷方式属于其他安装，原文件已保留".into());
            }
            let properties: IPropertyStore = link.cast().map_err(|_| "无法核对提醒入口标识")?;
            let existing =
                unsafe { properties.GetValue(&ACTIVATOR) }.map_err(|_| "无法核对提醒入口标识")?;
            if unsafe { PropVariantToGUID(&existing) }.is_ok_and(|value| value != STUB) {
                return Err("已有提醒入口使用其他激活方式，原文件已保留".into());
            }
            link
        } else {
            unsafe { CoCreateInstance(&ShellLink, None, CLSCTX_INPROC_SERVER) }
                .map_err(|_| "无法准备应用快捷方式")?
        };
        let user = RegKey::predef(HKEY_CURRENT_USER);
        let program = self.program_text()?;
        for path in [self.app_key(), self.protocol_key()] {
            let (key, _) = user
                .create_subkey(path)
                .map_err(|_| "无法保存当前用户的提醒关联")?;
            key.set_value("MewDexOwner", &self.app_id)
                .and_then(|_| key.set_value("MewDexProgram", &program))
                .map_err(|_| "无法保存提醒关联的来源")?;
        }
        let app = user
            .open_subkey_with_flags(self.app_key(), winreg::enums::KEY_WRITE)
            .map_err(|_| "无法保存系统提醒名称")?;
        app.set_value("DisplayName", &self.title)
            .map_err(|_| "无法保存系统提醒名称")?;
        let protocol = user
            .open_subkey_with_flags(self.protocol_key(), winreg::enums::KEY_WRITE)
            .map_err(|_| "无法保存提醒的对话入口")?;
        protocol
            .set_value("", &format!("URL:{} notification", self.title))
            .and_then(|_| protocol.set_value("URL Protocol", &""))
            .map_err(|_| "无法保存提醒的对话入口")?;
        let (command, _) = protocol
            .create_subkey("shell\\open\\command")
            .map_err(|_| "无法保存提醒的启动方式")?;
        command
            .set_value("", &self.command()?)
            .map_err(|_| "无法保存提醒的启动方式")?;
        unsafe {
            link.SetPath(&HSTRING::from(self.program_text()?))
                .map_err(|_| "无法保存快捷方式目标")?;
            let properties: IPropertyStore = link.cast().map_err(|_| "无法保存快捷方式标识")?;
            properties
                .SetValue(&AUMID, &PROPVARIANT::from(self.app_id.as_str()))
                .map_err(|_| "无法保存应用标识")?;
            properties
                .SetValue(
                    &ACTIVATOR,
                    &InitPropVariantFromCLSID(&STUB).map_err(|_| "无法保存提醒入口标识")?,
                )
                .map_err(|_| "无法保存提醒入口标识")?;
            properties.Commit().map_err(|_| "无法保存快捷方式标识")?;
            let persist: IPersistFile = link.cast().map_err(|_| "无法保存应用快捷方式")?;
            persist
                .Save(
                    &HSTRING::from(self.shortcut.to_str().ok_or("快捷方式路径无效")?),
                    true,
                )
                .map_err(|_| "无法保存应用快捷方式")?;
        }
        if !self.ready()? {
            return Err("系统提醒关联仍需核对，请重新检查".into());
        }
        Ok(())
    }

    #[cfg(feature = "notification-risk")]
    pub(crate) fn risk_paths(&self) -> (PathBuf, [String; 2]) {
        (self.shortcut.clone(), [self.app_key(), self.protocol_key()])
    }
}
