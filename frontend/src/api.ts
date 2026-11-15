export async function checkHealth(): Promise<string> {
  const response = await fetch("/api/health", {
    signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw new Error(`API вернул HTTP ${response.status}`);
  const body: unknown = await response.json();
  if (
    typeof body !== "object" || body === null ||
    !("status" in body) || body.status !== "ok" ||
    !("service" in body) || body.service !== "ai-buddy-api"
  ) throw new Error("Неожиданный ответ API");
  return "Backend доступен";
}
