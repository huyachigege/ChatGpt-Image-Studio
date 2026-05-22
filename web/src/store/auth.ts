"use client";

import localforage from "localforage";

export const AUTH_KEY_STORAGE_KEY = "chatgpt2api_auth_key";
export const AUTH_USER_STORAGE_KEY = "chatgpt2api_auth_user";

export type AuthUser = {
  id: string;
  username?: string;
  email?: string;
  name?: string;
  role: "admin" | "user" | string;
  imageApiKey?: string;
  disabled?: boolean;
};

const authStorage = localforage.createInstance({
  name: "chatgpt2api",
  storeName: "auth",
});

let cachedAuthKey = "";

export function getCachedAuthKey() {
  return cachedAuthKey;
}

export async function getStoredAuthKey() {
  if (typeof window === "undefined") {
    return "";
  }
  const value = await authStorage.getItem<string>(AUTH_KEY_STORAGE_KEY);
  cachedAuthKey = String(value || "").trim();
  return cachedAuthKey;
}

export async function setStoredAuthKey(authKey: string) {
  const normalizedAuthKey = String(authKey || "").trim();
  if (!normalizedAuthKey) {
    await clearStoredAuthKey();
    return;
  }
  cachedAuthKey = normalizedAuthKey;
  await authStorage.setItem(AUTH_KEY_STORAGE_KEY, normalizedAuthKey);
}

export async function getStoredAuthUser() {
  if (typeof window === "undefined") {
    return null;
  }
  return authStorage.getItem<AuthUser>(AUTH_USER_STORAGE_KEY);
}

export async function setStoredAuthUser(user: AuthUser | null) {
  if (!user) {
    await authStorage.removeItem(AUTH_USER_STORAGE_KEY);
    return;
  }
  await authStorage.setItem(AUTH_USER_STORAGE_KEY, user);
}

export async function clearStoredAuthKey() {
  if (typeof window === "undefined") {
    return;
  }
  cachedAuthKey = "";
  await authStorage.removeItem(AUTH_KEY_STORAGE_KEY);
  await authStorage.removeItem(AUTH_USER_STORAGE_KEY);
}
