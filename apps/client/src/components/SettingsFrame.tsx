import {
  Fragment,
  useId,
  useLayoutEffect,
  useRef,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { ResizableDialogContent } from "./ResizableDialogContent";

export function SettingsFrame<T extends string>({
  open,
  onOpenChange,
  title,
  description,
  sections,
  selected,
  onSelect,
  children,
  className = "",
  closeDisabled = false,
  closeLabel = "关闭设置",
  navigation = "sidebar",
  navigationLabel = "设置分类",
  footer,
  sizeKey = "settings",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  sections: {
    id: T;
    label: string;
    icon: ReactNode;
    disabled?: boolean;
    group?: string;
  }[];
  selected: T;
  onSelect: (id: T) => void;
  children: ReactNode;
  className?: string;
  closeDisabled?: boolean;
  closeLabel?: string;
  navigation?: "sidebar" | "tabs";
  navigationLabel?: string;
  footer?: ReactNode;
  sizeKey?: string;
}) {
  const id = useId();
  const returnFocus = useDialogReturnFocus();
  const body = useRef<HTMLDivElement>(null);
  const tabbed = navigation === "tabs";
  useLayoutEffect(() => {
    if (tabbed && open) body.current?.scrollTo({ top: 0 });
  }, [tabbed, selected, open]);
  function moveTab(event: KeyboardEvent<HTMLButtonElement>, section: T) {
    if (
      !tabbed ||
      event.nativeEvent.isComposing ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey
    )
      return;
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const available = sections.filter((item) => !item.disabled);
    const current = available.findIndex((item) => item.id === section);
    const next =
      event.key === "Home"
        ? available[0]
        : event.key === "End"
          ? available.at(-1)!
          : available[
              (current +
                (event.key === "ArrowRight" ? 1 : -1) +
                available.length) %
                available.length
            ];
    onSelect(next.id);
    document.getElementById(`${id}-${next.id}`)?.focus();
  }
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <ResizableDialogContent
          sizeKey={sizeKey}
          onOpenAutoFocus={returnFocus.capture}
          onCloseAutoFocus={returnFocus.restore}
          className={`settings-dialog settings-frame ${tabbed ? "settings-frame-tabs" : ""} ${className}`}
        >
          {(sizeControls) => (
            <>
              <header className="settings-frame-heading">
                <div>
                  <Dialog.Title>{title}</Dialog.Title>
                  <Dialog.Description>{description}</Dialog.Description>
                </div>
                <div className="settings-frame-actions">
                  {sizeControls}
                  <Dialog.Close
                    className="icon-button"
                    aria-label={closeLabel}
                    disabled={closeDisabled}
                  >
                    <X size={18} />
                  </Dialog.Close>
                </div>
              </header>
              <div className="settings-frame-layout">
                <nav
                  className="settings-navigation"
                  role={tabbed ? "tablist" : undefined}
                  aria-label={navigationLabel}
                >
                  {sections.map((section, index) => (
                    <Fragment key={section.id}>
                      {section.group &&
                        section.group !== sections[index - 1]?.group && (
                          <span className="settings-navigation-group">
                            {section.group}
                          </span>
                        )}
                      <button
                        id={`${id}-${section.id}`}
                        role={tabbed ? "tab" : undefined}
                        aria-current={
                          !tabbed && section.id === selected
                            ? "page"
                            : undefined
                        }
                        aria-selected={
                          tabbed ? section.id === selected : undefined
                        }
                        aria-controls={tabbed ? `${id}-panel` : undefined}
                        tabIndex={
                          tabbed
                            ? section.id === selected
                              ? 0
                              : -1
                            : undefined
                        }
                        disabled={section.disabled}
                        onClick={() => onSelect(section.id)}
                        onKeyDown={(event) => moveTab(event, section.id)}
                      >
                        {section.icon}
                        <span>{section.label}</span>
                      </button>
                    </Fragment>
                  ))}
                </nav>
                <div
                  className="settings-frame-body"
                  ref={body}
                  id={`${id}-panel`}
                  role={tabbed ? "tabpanel" : undefined}
                  aria-labelledby={tabbed ? `${id}-${selected}` : undefined}
                  tabIndex={tabbed ? 0 : undefined}
                >
                  {children}
                </div>
              </div>
              {footer && (
                <footer className="settings-frame-footer">{footer}</footer>
              )}
            </>
          )}
        </ResizableDialogContent>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
