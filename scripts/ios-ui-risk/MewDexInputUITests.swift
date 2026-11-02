import XCTest

// Development-only runner for the existing MewDex input application. It never
// uses a real device or official account login UI. System file/photo selectors
// are inspected only when opened by MewDex on the recorded owned simulator.
final class MewDexInputUITests: XCTestCase {
    @MainActor
    private func capture(_ app: XCUIApplication, _ stage: String) {
        let attachment = XCTAttachment(string: app.debugDescription)
        attachment.name = "MewDex input accessibility " + stage
        attachment.lifetime = .keepAlways
        add(attachment)
        let image = XCTAttachment(screenshot: app.screenshot())
        image.name = "MewDex input screen " + stage
        image.lifetime = .keepAlways
        add(image)
    }

    @MainActor
    private func replace(_ field: XCUIElement, with text: String) {
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        // The value fits in these single-line fields. Tap the trailing blank
        // area to put the caret after the existing value before deleting it.
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        let existing = field.value as? String ?? ""
        if !existing.isEmpty && existing != field.placeholderValue {
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: existing.count))
        }
        field.typeText(text)
        XCTAssertEqual(field.value as? String, text)
    }

    @MainActor
    private func openConversation(_ app: XCUIApplication, prompt: String) {
        XCTAssertTrue(app.buttons["打开设备列表"].waitForExistence(timeout: 30))
        app.buttons["打开设备列表"].tap()
        let conversation = app.buttons[String(prompt.prefix(40))]
        XCTAssertTrue(conversation.waitForExistence(timeout: 15))
        conversation.tap()
    }

    @MainActor
    private func openNotificationCenter(_ system: XCUIApplication) {
        let top = system.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.01))
        let lower = system.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
        top.press(forDuration: 0.1, thenDragTo: lower)
        capture(system, "notification-center-cold")
    }

    @MainActor
    private func openDeliveredNotification(_ app: XCUIApplication, system: XCUIApplication, cold: Bool) {
        let body = system.staticTexts.matching(NSPredicate(format: "label == %@", "一轮对话已完成。"))
        XCTAssertTrue(body.firstMatch.waitForExistence(timeout: 5))
        body.firstMatch.tap()
        if cold && !app.wait(for: .runningForeground, timeout: 3) {
            // A collapsed stack first expands its cards. Observe the expanded
            // hierarchy, then tap the upper visible card from this app only.
            capture(system, "notification-group-expanded")
            let cards = system.buttons.matching(NSPredicate(format: "identifier == %@ AND label CONTAINS %@ AND label CONTAINS %@", "ListCell", "MewDex", "一轮对话已完成。"))
            let visible = cards.allElementsBoundByIndex.filter { $0.isHittable }.sorted { $0.frame.minY < $1.frame.minY }
            XCTAssertFalse(visible.isEmpty)
            // This simulator's system routing explicitly reports "Hinting
            // side swipe instead of executing action" for a plain card tap.
            // Follow its open gesture without changing biometric settings.
            visible[0].swipeRight()
            if !app.wait(for: .runningForeground, timeout: 3) {
                capture(system, "notification-open-control")
                let open = system.buttons.matching(NSPredicate(format: "label IN %@", ["打开", "Open"]))
                XCTAssertEqual(open.count, 1)
                open.firstMatch.tap()
            }
        }
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 30))
    }

    @MainActor
    private func openFixtureNotification(_ app: XCUIApplication, system: XCUIApplication, cold: Bool) {
        openDeliveredNotification(app, system: system, cold: cold)
        XCTAssertTrue(app.staticTexts["界面检查模拟回复 120；仅用于历史分页，不表示模型执行。"].waitForExistence(timeout: 30))
        capture(app, cold ? "notification-opened-fixture-cold" : "notification-opened-fixture-warm")
    }

    @MainActor
    func testInputLifecycle() throws {
        continueAfterFailure = false
        let app = XCUIApplication(bundleIdentifier: "app.mewdex.client")
        let environment = ProcessInfo.processInfo.environment
        let mode = environment["MEWDEX_UI_MODE"]
        if mode == "notification-revoked" {
            guard let prompt = environment["MEWDEX_UI_PROMPT"], let reply = environment["MEWDEX_UI_REPLY"] else {
                XCTFail("The original retained input plan is required")
                return
            }
            XCTAssertEqual(app.state, .notRunning)
            XCUIDevice.shared.press(.home)
            let system = XCUIApplication(bundleIdentifier: "com.apple.springboard")
            openNotificationCenter(system)
            openDeliveredNotification(app, system: system, cold: true)
            XCTAssertTrue(app.buttons["打开设备列表"].waitForExistence(timeout: 30))
            XCTAssertFalse(app.staticTexts["界面检查模拟回复 120；仅用于历史分页，不表示模型执行。"].exists)
            app.buttons["打开设备列表"].tap()
            XCTAssertFalse(app.buttons["界面检查：历史与冻结队列（模拟记录）"].exists)
            XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "windows/amd64")).count, 1)
            let original = app.buttons[String(prompt.prefix(40))]
            XCTAssertTrue(original.waitForExistence(timeout: 15))
            original.tap()
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            capture(app, "revoked-notification-ignored-original-history-available")
            return // A delivered notification cannot restore removed access.
        }
        if mode == "notification-cold-open" {
            XCTAssertEqual(app.state, .notRunning)
            XCUIDevice.shared.press(.home)
            let system = XCUIApplication(bundleIdentifier: "com.apple.springboard")
            capture(system, "notification-retained-app-not-running")
            openNotificationCenter(system)
            openFixtureNotification(app, system: system, cold: true)
            return // Open the retained delivered card; emit no new event.
        }
        app.launch()
        XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 30))
        guard mode == "network" || mode == "recover" || mode == "viewport" || mode == "attachment-picker" || mode == "attachment-files" || mode == "attachment-file-upload" || mode == "attachment-gallery" || mode == "attachment-upload" || mode == "notification-settings" || mode == "notification-enable" || mode == "notification-disable" || mode == "notification-warm" || mode == "notification-cold" || mode == "notification-offline" else {
            capture(app, "startup")
            return
        }
        guard let hub = environment["MEWDEX_UI_HUB"], let code = environment["MEWDEX_UI_PAIR_CODE"],
              let name = environment["MEWDEX_UI_NAME"], let prompt = environment["MEWDEX_UI_PROMPT"],
              let reply = environment["MEWDEX_UI_REPLY"] else {
            XCTFail("An explicit private network plan is required")
            return
        }
        if mode == "notification-settings" || mode == "notification-enable" || mode == "notification-disable" {
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            app.buttons["打开设备列表"].tap()
            let settings = app.buttons["设备与连接"]
            XCTAssertTrue(settings.waitForExistence(timeout: 15))
            settings.tap()
            XCTAssertTrue(app.staticTexts["工作台提醒"].waitForExistence(timeout: 15))
            capture(app, "notification-settings")
            if mode == "notification-enable" {
                let toggle = app.switches["任务完成与待处理请求"]
                XCTAssertTrue(toggle.waitForExistence(timeout: 15))
                XCTAssertEqual(toggle.value as? String, "0")
                XCTAssertTrue(toggle.isEnabled)
                toggle.tap()
                let system = XCUIApplication(bundleIdentifier: "com.apple.springboard")
                let permission = system.alerts.firstMatch
                if permission.waitForExistence(timeout: 10) {
                    capture(system, "mewdex-notification-permission")
                    XCTAssertTrue(permission.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "MewDex")).firstMatch.exists)
                    let allow = permission.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"]))
                    XCTAssertEqual(allow.count, 1)
                    allow.firstMatch.tap()
                }
                let enabled = NSPredicate(format: "value == %@", "1")
                expectation(for: enabled, evaluatedWith: toggle)
                waitForExpectations(timeout: 20)
                capture(app, "notification-enabled")
                app.buttons["关闭网络设置"].tap()
            }
            if mode == "notification-disable" {
                let toggle = app.switches["任务完成与待处理请求"]
                XCTAssertEqual(toggle.value as? String, "1")
                toggle.tap()
                expectation(for: NSPredicate(format: "value == %@", "0"), evaluatedWith: toggle)
                waitForExpectations(timeout: 10)
                capture(app, "notification-disabled")
            }
            return
        }
        if mode == "notification-offline" {
            guard let eventID = environment["MEWDEX_UI_NOTIFICATION_ID"] else {
                XCTFail("An explicit labelled fixture event is required")
                return
            }
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            app.terminate()
            XCTAssertEqual(app.state, .notRunning)
            NSLog("MEWDEX_NOTIFICATION_OFFLINE_READY_%@", eventID)
            Thread.sleep(forTimeInterval: 45)
            XCTAssertEqual(app.state, .notRunning)
            let system = XCUIApplication(bundleIdentifier: "com.apple.springboard")
            capture(system, "notification-input-still-closed")
            app.launch()
            let body = system.staticTexts.matching(NSPredicate(format: "label == %@", "一轮对话已完成。"))
            XCTAssertTrue(body.firstMatch.waitForExistence(timeout: 30))
            capture(system, "notification-offline-result-synchronized")
            openFixtureNotification(app, system: system, cold: false)
            app.terminate()
            app.launch()
            XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 30))
            Thread.sleep(forTimeInterval: 7)
            XCTAssertFalse(body.firstMatch.exists)
            capture(app, "notification-no-duplicate-after-restart")
            return // Existing results synchronize; no user input is submitted.
        }
        if mode == "notification-warm" || mode == "notification-cold" {
            guard let eventID = environment["MEWDEX_UI_NOTIFICATION_ID"] else {
                XCTFail("An explicit labelled fixture event is required")
                return
            }
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            NSLog("MEWDEX_NOTIFICATION_READY_%@", eventID)
            let system = XCUIApplication(bundleIdentifier: "com.apple.springboard")
            let body = system.staticTexts.matching(NSPredicate(format: "label == %@", "一轮对话已完成。"))
            let appeared = body.firstMatch.waitForExistence(timeout: 60)
            capture(system, "notification-delivered-warm")
            XCTAssertTrue(appeared)
            XCTAssertEqual(body.count, 1)
            if mode == "notification-cold" {
                app.terminate()
                XCTAssertEqual(app.state, .notRunning)
                capture(system, "notification-app-not-running")
                // Closing the app dismisses its transient banner. Open the
                // real notification centre from this observed screen frame.
                openNotificationCenter(system)
                XCTAssertTrue(body.firstMatch.waitForExistence(timeout: 5))
            }
            openFixtureNotification(app, system: system, cold: mode == "notification-cold")
            XCTAssertFalse(app.staticTexts[reply].exists)
            return // Native notification only; never submit a model input.
        }
        if mode == "attachment-picker" || mode == "attachment-files" || mode == "attachment-file-upload" || mode == "attachment-gallery" || mode == "attachment-upload" {
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            let add = app.buttons["添加附件"]
            XCTAssertTrue(add.waitForExistence(timeout: 15))
            XCTAssertTrue(add.isHittable)
            add.tap()
            capture(app, "attachment-picker-options")
            if mode == "attachment-files" || mode == "attachment-file-upload" {
                let files = app.buttons["Choose Files"]
                XCTAssertTrue(files.waitForExistence(timeout: 15))
                files.tap()
                capture(app, "attachment-native-files")
                let documents = XCUIApplication(bundleIdentifier: "com.apple.DocumentManagerUICore.Service")
                XCTAssertTrue(documents.wait(for: .runningForeground, timeout: 15))
                capture(documents, "native-document-selector")
                let browse = documents.tabBars.buttons["浏览"]
                XCTAssertTrue(browse.waitForExistence(timeout: 15))
                browse.tap()
                capture(documents, "native-document-locations")
                let local = documents.cells["DOC.sidebar.item.我的iPhone"]
                if local.waitForExistence(timeout: 2) { local.tap() }
                XCTAssertTrue(documents.staticTexts["我的iPhone"].waitForExistence(timeout: 10))
                capture(documents, "native-local-documents")
                if mode == "attachment-file-upload" {
                    guard let name = environment["MEWDEX_UI_DOCUMENT_NAME"], name.hasSuffix(".txt") else {
                        XCTFail("An explicit synthetic document is required")
                        return
                    }
                    let stem = String(name.dropLast(4))
                    let rows = documents.collectionViews["File View"].cells.matching(NSPredicate(format: "label BEGINSWITH %@", stem))
                    XCTAssertTrue(rows.firstMatch.waitForExistence(timeout: 15))
                    XCTAssertEqual(rows.count, 1)
                    rows.firstMatch.tap()
                    capture(documents, "synthetic-document-selected")
                    let open = documents.buttons.matching(NSPredicate(format: "label IN %@", ["Open", "打开"]))
                    if open.firstMatch.waitForExistence(timeout: 2) {
                        XCTAssertEqual(open.count, 1)
                        XCTAssertTrue(open.firstMatch.isEnabled)
                        open.firstMatch.tap()
                    }
                    let removal = app.buttons["移除附件 " + name]
                    XCTAssertTrue(removal.waitForExistence(timeout: 60))
                    XCTAssertTrue(app.buttons["发送消息"].isEnabled)
                    capture(app, "synthetic-text-attachment-added")
                    removal.tap()
                    XCTAssertFalse(removal.exists)
                    XCTAssertFalse(app.buttons["发送消息"].isEnabled)
                    capture(app, "synthetic-text-draft-cleared")
                }
                return // Observe the native selector without submitting input.
            }
            if mode == "attachment-gallery" || mode == "attachment-upload" {
                let library = app.buttons["Photo Library"]
                XCTAssertTrue(library.waitForExistence(timeout: 15))
                library.tap()
                capture(app, "attachment-photo-library")
                let picker = XCUIApplication(bundleIdentifier: "com.apple.mobileslideshow.photospicker")
                capture(picker, "native-photo-library")
                if mode == "attachment-upload" {
                    guard let photoLabel = environment["MEWDEX_UI_PHOTO_LABEL"], !photoLabel.isEmpty else {
                        XCTFail("Select only the previously observed synthetic photo label")
                        return
                    }
                    let photos = picker.images.matching(NSPredicate(format: "label == %@", photoLabel))
                    XCTAssertTrue(photos.firstMatch.waitForExistence(timeout: 15))
                    XCTAssertEqual(photos.count, 1)
                    // Extension-hosted thumbnails expose their observed frame
                    // but report no accessibility hit point on this runtime.
                    photos.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
                    let done = picker.buttons["完成"]
                    XCTAssertTrue(done.isEnabled)
                    done.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() // Never tap model Send.
                    let removals = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "移除附件 "))
                    XCTAssertTrue(removals.firstMatch.waitForExistence(timeout: 60))
                    XCTAssertEqual(removals.count, 1)
                    XCTAssertTrue(removals.firstMatch.isHittable)
                    let send = app.buttons["发送消息"]
                    XCTAssertTrue(send.isEnabled)
                    capture(app, "synthetic-attachment-added")
                    removals.firstMatch.tap() // Remove only this unsent draft reference.
                    XCTAssertFalse(removals.firstMatch.exists)
                    XCTAssertFalse(send.isEnabled)
                    capture(app, "synthetic-attachment-draft-cleared")
                }
            }
            return // No model input in any attachment mode.
        }
        if mode == "viewport" {
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            let input = app.textViews.firstMatch
            XCTAssertTrue(input.waitForExistence(timeout: 15))
            input.tap()
            let firstLine = "Unsent input layout draft. Do not use tools."
            input.typeText(firstLine)
            input.typeText("\nSecond draft line.")
            XCTAssertEqual(input.value as? String, firstLine + "\nSecond draft line.")
            let keyboard = app.keyboards.firstMatch
            XCTAssertTrue(keyboard.waitForExistence(timeout: 15))
            let header = app.buttons["打开设备列表"]
            XCTAssertTrue(header.isHittable)
            XCTAssertGreaterThanOrEqual(header.frame.minY, app.frame.minY)
            XCTAssertGreaterThanOrEqual(input.frame.minY, app.frame.minY)
            XCTAssertLessThanOrEqual(input.frame.maxY, keyboard.frame.minY)
            let send = app.buttons["发送消息"]
            XCTAssertTrue(send.isHittable)
            XCTAssertGreaterThanOrEqual(send.frame.minX, app.frame.minX)
            XCTAssertLessThanOrEqual(send.frame.maxX, app.frame.maxX)
            XCTAssertLessThanOrEqual(send.frame.maxY, keyboard.frame.minY)
            XCTAssertTrue(app.staticTexts[reply].isHittable)
            capture(app, "keyboard-viewport-multiline")
            app.terminate() // Discard only this explicitly unsent test draft.
            app.launch()
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            XCTAssertTrue(app.buttons["打开设备列表"].isHittable)
            XCTAssertFalse(app.keyboards.firstMatch.exists)
            capture(app, "viewport-after-relaunch")
            return
        }
        if mode == "recover" {
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            XCUIDevice.shared.press(.home)
            Thread.sleep(forTimeInterval: 2)
            app.activate()
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            capture(app, "existing-reply-after-background")
            app.terminate()
            app.launch()
            openConversation(app, prompt: prompt)
            XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
            XCTAssertTrue(app.staticTexts["就绪"].exists)
            capture(app, "existing-history-after-relaunch")
            return // No pairing, create, start, steer or enqueue in recovery.
        }
        if environment["MEWDEX_UI_ALREADY_PAIRED"] != "1" {
            replace(app.textFields["主控地址"], with: hub)
            replace(app.textFields["输入端名称"], with: name)
            replace(app.textFields["一次性配对码"], with: code)
            app.buttons["连接设备网络"].tap()
        }
        XCTAssertTrue(app.buttons["打开设备列表"].waitForExistence(timeout: 30))
        capture(app, "paired")

        app.terminate()
        app.launch()
        XCTAssertTrue(app.buttons["打开设备列表"].waitForExistence(timeout: 30))
        app.buttons["打开设备列表"].tap()
        XCTAssertFalse(app.buttons["本机设备"].exists)
        XCTAssertFalse(app.buttons["停止本机服务"].exists)
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "windows/amd64")).count, 1)
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "darwin/arm64")).count, 0)
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "linux/amd64")).count, 0)
        app.buttons["关闭设备列表"].tap()
        let input = app.textViews.firstMatch
        XCTAssertTrue(input.waitForExistence(timeout: 15))
        input.tap()
        input.typeText(prompt)
        XCTAssertEqual(input.value as? String, prompt)
        let send = app.buttons["发送消息"]
        XCTAssertTrue(send.isEnabled)
        capture(app, "keyboard-before-send")
        XCTAssertTrue(send.isHittable)
        XCTAssertGreaterThanOrEqual(send.frame.minX, app.frame.minX)
        XCTAssertLessThanOrEqual(send.frame.maxX, app.frame.maxX)
        send.tap() // This plan is never automatically retried after this step.
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label == %@", prompt)).firstMatch.waitForExistence(timeout: 30))
        XCUIDevice.shared.press(.home)
        Thread.sleep(forTimeInterval: 2)
        app.activate()
        XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 60))
        XCTAssertTrue(app.staticTexts["就绪"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.staticTexts[reply].isHittable)
        capture(app, "reply-after-background")

        app.terminate()
        app.launch()
        openConversation(app, prompt: prompt)
        XCTAssertTrue(app.staticTexts[reply].waitForExistence(timeout: 30))
        capture(app, "history-after-relaunch")
    }
}
