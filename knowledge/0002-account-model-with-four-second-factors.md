# ADR-0002: Account Model with Four Second Factors

- **Status**: accepted
- **Date**: 2026-11-04

## Context

A general-purpose authentication core needs more than password login. The core
had to support several independent second-factor mechanisms and let a user hold
any combination of them, while keeping login flow logic in one place.

## Decision

The `users` model carries per-factor state and the core implements **four**
second factors:

1. **TOTP** — `TOTP` secret + `2FA` toggle column; setup/toggle in
   `controllers/totp_controller.go`, verification in
   `controllers/user_security_controller.go` (`verifyTOTP`, 30s window).
2. **WebAuthn** — `WebAuthn2FAEnabled` flag plus a `webauthn_credentials`
   table; registration/second-factor flows in
   `controllers/webauthn_controller.go`.
3. **Email 2FA** — `Email2FAEnabled` flag; one-time codes sent/verified in
   `controllers/email_2fa_controller.go` (code deleted after use).
4. **Recovery key** — `RecoveryKey` (bcrypt hash) + `RecoveryKeyEnabled`;
   generated as 4×4 uppercase groups, plaintext shown exactly once.

A single combined status endpoint reports all factors; the older per-factor
status endpoints were removed.

**Recovery key is a backup recovery method, not an active login MFA factor.**
Direct token issuance is blocked only when TOTP/WebAuthn/Email-2FA is enabled
and no factor was selected; merely having a recovery key no longer forces the
2FA flow.

## Consequences

- A user with only a recovery key configured logs in directly; the recovery key
  remains available as an optional factor that hook policies may still require.
- The recovery key plaintext is returned once and stored only as a bcrypt hash;
  losing it requires regeneration.
- **Fixed (commit `f12e42a`)**: `verifyAnyFactor`'s email-code branch used to
  accept any valid code from the shared file-based code store without
  re-checking `Email2FAEnabled`. Impact before the fix: with Email 2FA turned
  off, a valid code — which `SendChangeEmailCode` issues unconditionally and
  which is shared across email flows — still passed the second-factor gate on
  `ChangeEmail`, `RegenerateRecoveryKey`, and `RevokeRecoveryKey`. The branch
  now requires `Email2FAEnabled` and a non-empty email, and returns
  `400 email_2fa_not_configured` otherwise, matching the enablement checks of
  the TOTP and recovery key branches.
- Combining factors is additive state on one row; there is no per-factor
  enrollment table except WebAuthn credentials.

## Evidence

- Model fields: `models/models.go::User` (`TOTP`, `TwoFA`/column `2FA`,
  `WebAuthn2FAEnabled`, `Email2FAEnabled`, `RecoveryKey`,
  `RecoveryKeyEnabled`); WebAuthn credential table
  `models/models.go::WebAuthnCredential`; DDL
  `database/migrations/000001_baseline.up.sql::recovery_key_enabled` (users
  table 2FA columns).
- TOTP: `controllers/totp_controller.go::SetupTOTP`,
  `controllers/totp_controller.go::Toggle2FA` (`Update("2FA", ...)`); verify
  `controllers/user_security_controller.go::verifyTOTP`.
- WebAuthn: `controllers/webauthn_controller.go::BeginRegistration` /
  `controllers/webauthn_controller.go::FinishRegistration` (register
  begin/finish), `controllers/webauthn_controller.go::Toggle2FA`,
  `controllers/webauthn_controller.go::BeginSecondFactor` /
  `controllers/webauthn_controller.go::FinishSecondFactor` (second-factor
  begin/finish); login gating `controllers/oauth2_controller.go::LoginTicket`
  (factor detection block).
- Email 2FA: `controllers/email_2fa_controller.go::SendCode`,
  `controllers/email_2fa_controller.go::VerifyCode`,
  `controllers/email_2fa_controller.go::Toggle` (requires verified email).
- Recovery key: `controllers/user_security_controller.go::CreateRecoveryKey`
  (create, plaintext returned once),
  `controllers/user_security_controller.go::RegenerateRecoveryKey` (regenerate),
  `controllers/user_security_controller.go::RevokeRecoveryKey` (revoke),
  `controllers/user_security_controller.go::VerifyRecoveryKey` (verify via
  login ticket),
  `controllers/user_security_controller.go::generateRecoveryKey`.
- Combined status endpoint:
  `controllers/user_security_controller.go::GetTwoFactorStatus` (response
  fields), route `main.go::GetTwoFactorStatus`; commit `e846507` removed the
  three separate status endpoints.
- Recovery key excluded from direct-issuance gating:
  `controllers/oauth2_controller.go::LoginTicket` (factor gate); commit
  `977bd12` ("recovery key is a backup recovery method rather than an active
  login MFA factor"). Recovery key still listed for hook policy via
  `availableSecondFactors`
  (`controllers/oauth2_controller.go::availableSecondFactors`).
- Factor dispatch: `controllers/user_security_controller.go::verifyAnyFactor`
  (branches: TOTP / email / WebAuthn / recovery key); email branch enablement
  gate inside it (since commit `f12e42a`).
- Email-factor gate fix and regression tests: commit `f12e42a`
  (`controllers/user_security_controller_test.go`: disabled flag rejects a
  stored code without consuming it; enabled flag accepts and consumes it;
  wrong code rejected; recovery key branch unaffected).
- Commits: `9777cf6` (webauthn 2fa field), `d08f523` (recovery key status),
  `e846507`, `977bd12`, `f12e42a` (email factor gate fix).
