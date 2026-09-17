export interface BrowserNotificationLike {
  onclick: ((event: Event) => void) | null;
  close: () => void;
}

export interface BrowserNotificationEnvironment {
  getPermission: () => NotificationPermission;
  requestPermission: () => Promise<NotificationPermission>;
  createNotification: (title: string, options?: NotificationOptions) => BrowserNotificationLike;
}

function defaultEnvironment(): BrowserNotificationEnvironment | null {
  if (typeof window === "undefined" || !("Notification" in window)) return null;
  return {
    getPermission: () => Notification.permission,
    requestPermission: () => Notification.requestPermission(),
    createNotification: (title, options) => new Notification(title, options),
  };
}

export async function requestBrowserNotificationPermission(
  environment: BrowserNotificationEnvironment | null = defaultEnvironment(),
): Promise<NotificationPermission | null> {
  if (!environment) return null;
  const permission = environment.getPermission();
  if (permission !== "default") return permission;
  try {
    return await environment.requestPermission();
  } catch {
    return null;
  }
}

export async function showBrowserNotification(
  options: { title: string; body: string; onClick: () => void },
  environment: BrowserNotificationEnvironment | null = defaultEnvironment(),
): Promise<boolean> {
  if (!environment || environment.getPermission() !== "granted") return false;
  try {
    const notification = environment.createNotification(options.title, { body: options.body });
    notification.onclick = () => {
      notification.close();
      options.onClick();
    };
    return true;
  } catch {
    return false;
  }
}
