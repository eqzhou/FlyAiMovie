/**
 * Extracts a user-facing message from an unknown thrown value.
 *
 * `catch` binds `unknown`, so every call site previously repeated
 * `reason instanceof Error ? reason.message : '<fallback>'`. The fallback is
 * required rather than defaulted: a generic default would quietly replace the
 * specific Chinese copy each surface shows today.
 */
export function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback
}
