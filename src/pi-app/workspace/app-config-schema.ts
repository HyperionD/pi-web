import { z } from "zod";

export const DEFAULT_APP_BRAND = "pi.dev / web";

const appConfigResponseSchema = z.object({
  config: z.object({
    version: z.number(),
    brand: z.string(),
  }).passthrough(),
  path: z.string(),
}).passthrough();

export type AppConfigResponse = z.infer<typeof appConfigResponseSchema>;

function validationMessage(prefix: string, error: z.ZodError) {
  const details = error.issues.map((issue) => `${issue.path.join(".") || "value"}: ${issue.message}`).join("; ");
  return `${prefix}: ${details}`;
}

export function parseAppConfigResponse(value: unknown): AppConfigResponse {
  const result = appConfigResponseSchema.safeParse(value);
  if (!result.success) throw new Error(validationMessage("invalid app config response", result.error));
  return result.data;
}
