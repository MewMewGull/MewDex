// Copyright 2026 MewMewGull and MewDex contributors
// SPDX-License-Identifier: Apache-2.0

import XCTest
import UserNotifications
@testable import tauri_plugin_notification

final class MewDexNotificationTests: XCTestCase {
  func testDeliveredMetadataSurvivesAnEmptyProcessCache() throws {
    let content = UNMutableNotificationContent()
    content.title = "MewDex"
    content.body = "一轮对话已完成。"
    content.categoryIdentifier = "mewdex-conversation"
    content.userInfo["__EXTRA__"] = [
      "scope": "owned-input-scope", "deviceId": "node", "sessionId": "session"
    ]
    let request = UNNotificationRequest(identifier: "73", content: content, trigger: nil)
    // A new handler has no in-memory record of the original delivery.
    let actual = NotificationHandler().toActiveNotification(request)
    let encoded = try JSONEncoder().encode(actual)
    let value = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    let extra = try XCTUnwrap(value["extra"] as? [String: String])
    XCTAssertEqual(extra["scope"], "owned-input-scope")
    XCTAssertEqual(extra["deviceId"], "node")
    XCTAssertEqual(extra["sessionId"], "session")
    XCTAssertEqual(value["id"] as? Int, 73)
    XCTAssertEqual(value["actionTypeId"] as? String, "mewdex-conversation")
  }

  func testAnOldNotificationWithoutExtrasDoesNotRequireAMemoryRecord() throws {
    let content = UNMutableNotificationContent()
    content.title = "old notification"
    let request = UNNotificationRequest(identifier: "old-delivery", content: content, trigger: nil)
    let actual = NotificationHandler().toActiveNotification(request)
    let encoded = try JSONEncoder().encode(actual)
    let value = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    XCTAssertEqual(value["id"] as? Int, -1)
    XCTAssertNil(value["extra"])
  }
}
