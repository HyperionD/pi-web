import { afterEach, describe, expect, it, vi } from "vitest";
import {
  requestBrowserNotificationPermission,
  showBrowserNotification,
  type BrowserNotificationEnvironment,
} from "./browser-notifications";

type FakeNotification = {
  onclick: ((event: Event) => void) | null;
  close: ReturnType<typeof vi.fn>;
};

function environment(permission: NotificationPermission = "granted") {
  const notifications: Array<{ title: string; options?: NotificationOptions }> = [];
  const instances: FakeNotification[] = [];
  const env: BrowserNotificationEnvironment = {
    getPermission: () => permission,
    requestPermission: vi.fn(async () => permission),
    createNotification: vi.fn((title, options) => {
      const notification = { onclick: null, close: vi.fn() };
      instances.push(notification);
      notifications.push({ title, options });
      return notification;
    }),
  };
  return { env, notifications, instances };
}

describe("browser notifications", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows a system notification with a clickable session target", async () => {
    const { env, notifications, instances } = environment();
    const onClick = vi.fn();

    const delivery = await showBrowserNotification({
      title: "回答完成",
      body: "demo · chat · explain the failure",
      onClick,
    }, env);

    expect(delivery).toBe(true);
    expect(notifications).toEqual([{
      title: "回答完成",
      options: { body: "demo · chat · explain the failure" },
    }]);
    instances[0].onclick?.(new Event("click"));
    expect(instances[0].close).toHaveBeenCalledOnce();
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("does not request permission from an asynchronous notification event", async () => {
    const { env, notifications } = environment("default");

    const delivery = await showBrowserNotification({ title: "回答完成", body: "完成", onClick: vi.fn() }, env);

    expect(delivery).toBe(false);
    expect(env.requestPermission).not.toHaveBeenCalled();
    expect(notifications).toHaveLength(0);
  });

  it("does not show when permission is denied", async () => {
    const { env, notifications } = environment("denied");

    const delivery = await showBrowserNotification({ title: "失败", body: "失败", onClick: vi.fn() }, env);

    expect(delivery).toBe(false);
    expect(notifications).toHaveLength(0);
  });

  it("reports whether permission can be requested", async () => {
    const { env } = environment("default");
    env.requestPermission = vi.fn(async (): Promise<NotificationPermission> => "denied");

    await expect(requestBrowserNotificationPermission(env)).resolves.toBe("denied");
    expect(env.requestPermission).toHaveBeenCalledOnce();
  });

  it("returns no permission when the browser rejects the permission request", async () => {
    const { env } = environment("default");
    env.requestPermission = vi.fn(async (): Promise<NotificationPermission> => {
      throw new Error("permission blocked");
    });

    await expect(requestBrowserNotificationPermission(env)).resolves.toBeNull();
  });

  it("silently handles unsupported browsers and notification construction errors", async () => {
    await expect(showBrowserNotification({ title: "完成", body: "完成", onClick: vi.fn() }, null)).resolves.toBe(false);

    const { env } = environment();
    env.createNotification = vi.fn(() => {
      throw new Error("unsupported constructor");
    });
    await expect(showBrowserNotification({ title: "完成", body: "完成", onClick: vi.fn() }, env)).resolves.toBe(false);
  });
});
