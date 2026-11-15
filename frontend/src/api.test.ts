import { afterEach, expect, it, vi } from "vitest";
import { checkHealth } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("accepts the health contract", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ status: "ok", service: "ai-buddy-api" })),
  ));
  await expect(checkHealth()).resolves.toBe("Backend доступен");
});

it("does not present HTTP failure as healthy", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 503 })));
  await expect(checkHealth()).rejects.toThrow("HTTP 503");
});

it("rejects an unrelated service response", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"status":"ok"}')));
  await expect(checkHealth()).rejects.toThrow("Неожиданный ответ");
});
