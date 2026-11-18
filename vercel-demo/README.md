# CloudNative Feed — Vercel demo

This is a separate, standalone demo for Vercel. Its files live under `vercel-demo/`; the AWS/EKS application remains at the repository root and is not required to build or run this version. The Vercel app uses Go HTTP functions, static feed assets, and Supabase for durable PostgreSQL data and Auth.

The Go runtime on Vercel is currently Beta. A successful local build does not prove that the account's Vercel project or Hobby plan is eligible; deploy a preview and run the acceptance checklist before sharing it.

## What works in this version

- Public feed reads, photo likes, comments, and comment likes persist in Supabase.
- Likes are unique per signed visitor cookie; the raw cookie ID is not stored in the database.
- Supabase Auth credentials are submitted only to the sign-in endpoint. Passwords are never stored, logged, or returned by this app.
- Only active users mapped in `admin_access` as the owner or an admin can use the operations panel.
- The owner can enable/disable the three configured admin accounts, grant/revoke simulation permission, and send an Auth password-reset email.
- Real API request/error/latency/activity counts are stored in one-minute buckets for seven days; demo simulation records are retained for 30 days.
- CPU, memory, requests-per-second, replicas, HPA state, and alerts are explicitly simulated. The fixed 90-second scenario models 10 virtual users, scale-up, and cooldown using a persisted run record; it does not send load to Vercel or run Kubernetes/k6. Routine dashboard polling remains real app traffic.

Static image/font requests are not included in application metrics. Supabase secret-key access is server-side only; it must never be copied into browser code or static assets.

## Prerequisites

1. A Vercel account/project owner. Check the current Hobby plan's eligibility and fair-use terms for this demo before deploying. Do not select or enable a paid plan without the owner's approval.
2. A Supabase project on a plan whose limits and pause behavior are acceptable for a temporary demo. Verify the current signup requirements, quotas, and whether a payment method is requested before relying on it.
3. Go 1.24 or newer for local tests. Vercel reads the Go version from `go.mod`.
4. The owner and admins' verified email addresses should be entered only in Supabase's private invitation/account setup workflow. Do not put them in this repository.
5. Supabase Auth email delivery must be able to reach all four users. Supabase's default SMTP is restricted to project-team email addresses and is currently rate-limited; if these users are not team members, a separately approved custom SMTP provider is required. Do not assume invitations or password resets will work until an email is delivered successfully.

Free plans and card-free account signup are provider/account-dependent, not guaranteed by this source code. Supabase Free projects may pause after inactivity; check current provider policies and limits.
See [Supabase custom SMTP requirements](https://supabase.com/docs/guides/auth/auth-smtp) before account setup. Custom SMTP credentials, if used, belong only in the Supabase dashboard; confirm the mail provider's signup/payment requirements separately.

## Local checks

From this directory:

```powershell
go test ./...
go vet ./...
```

The app has no third-party Go module dependencies. For local preview, set the values named in `.env.example` using an ignored `.env.local` file or your local environment. Use a fresh random `VISITOR_HMAC_SECRET` of at least 32 bytes. Keep the service-role key and all other secrets out of Git and command history.

## Supabase setup

1. Create a Supabase project only after confirming the plan, limits, and signup/billing requirements.
2. In the Supabase SQL Editor, run `supabase/migrations/001_init.sql`.
3. Configure Auth email delivery first. The default SMTP server can deliver only to pre-authorized project-team addresses (and has a low send limit); use custom SMTP if these users are outside that team. Verify delivery before inviting.
4. In Supabase Auth URL Configuration, set the Site URL to the deployed app origin and add `https://<deployment-host>/admin/password-update` to the redirect URL allow list. Invite the owner and three admins individually, selecting that password-update URL as each invitation's redirect. Ask recipients not to open the invitation until their access mappings have been added in the next step. Each person sets their own password through the private invitation flow. Never ask for or share passwords.
5. Obtain each invited account's Auth user UUID from the Supabase dashboard. Run the following in the SQL Editor after replacing each placeholder with the matching UUID. Do not commit the completed query or UUIDs.

```sql
insert into public.admin_access
    (user_id, username, role, enabled, can_load_test)
values
    ('<OWNER_AUTH_USER_UUID>', 'g95rr', 'owner', true, true),
    ('<ADMIN_1_AUTH_USER_UUID>', 'drsura.dev', 'admin', true, false),
    ('<ADMIN_2_AUTH_USER_UUID>', 'admin2.dev', 'admin', true, false),
    ('<ADMIN_3_AUTH_USER_UUID>', 'admin3.dev', 'admin', true, false);
```

The `admin_access` table has RLS enabled and is not available to anonymous or ordinary authenticated clients. The application uses its server-only service-role secret to check and update these records. The migration enforces at most one enabled owner; the application does not expose an operation to disable or demote the owner.

The app's owner-triggered password-reset requests use `APP_BASE_URL/admin/password-update` as the recovery redirect. Configure the exact URL in Supabase Auth's redirect allow list as well as using it for invitations. The current app uses a direct email/password token exchange and does not require an OAuth callback.

## Vercel setup and preview

1. Import the repository into Vercel.
2. Set the Vercel **Root Directory** to `vercel-demo`. Do not deploy the repository root or `public-app/`.
3. Confirm the Go Framework preset is selected. `go.mod` and the Go function under `api/` are at the Vercel project root.
4. Add these environment variables in the Vercel project settings for Preview and Production as appropriate:
   - `APP_BASE_URL` (the exact Vercel deployment origin, without a path)
   - `SUPABASE_URL`
   - `SUPABASE_PUBLISHABLE_KEY`
   - `SUPABASE_SECRET_KEY` (server-only/sensitive; never expose to the browser)
   - `VISITOR_HMAC_SECRET` (random, at least 32 bytes)
   - `ADMIN_COOKIE_SECURE=true`
5. Deploy a Preview first. Verify static image delivery, feed reads, photo likes, comments, durable comment likes, admin login, role enforcement, metrics persistence, and the simulation's baseline → scale-up → cooldown transitions and history. Check function and Supabase logs for errors; never paste secret values into logs or chat.
6. Only promote a Preview after every item in the acceptance checklist below passes and provider usage is within the approved free-plan limits.

The Vercel project must use `vercel-demo/` as its root so it does not need AWS credentials, EKS, Cognito, Prometheus, or the original app's runtime. The Vercel Go runtime is Beta; if a Preview does not build or route correctly, stop and get approval before changing the language/framework or paid plan.

The app also accepts the legacy `SUPABASE_ANON_KEY` and `SUPABASE_SERVICE_ROLE_KEY` variables when neither new-format variable is set. Use one complete pair, not a mixture. New-format keys are sent only in Supabase's `apikey` header; legacy JWT keys continue to use both `apikey` and `Authorization` where needed. If a secret key was exposed, rotate it in Supabase before deploying and update the Vercel environment variable with the replacement.

## Acceptance checklist

- [ ] Feed and bundled images load for an anonymous visitor.
- [ ] Photo likes and comments survive refresh/redeployment.
- [ ] Comment likes survive refresh and do not increment twice for the same visitor cookie.
- [ ] Invalid/oversized comments and excess mutations are rejected.
- [ ] An anonymous visitor cannot access the admin dashboard APIs or run simulations.
- [ ] Each active mapped admin can log in; disabled/unmapped users are denied server-side.
- [ ] Only the owner can change admin access; an admin without permission cannot start the simulation, including by direct API request.
- [ ] Invitation and password-reset links open the password-update page; only enabled, mapped users can complete the update, and no password is exposed in logs or stored by the app.
- [ ] Real app metrics and simulated infrastructure values have distinct, visible labels.
- [ ] Starting the simulation shows the fixed virtual-user scenario, simulated request rate, CPU/replica scale-up, recovery, and a persisted history record.
- [ ] The simulation does not generate synthetic requests to Vercel; dashboard polling remains ordinary real app traffic.
- [ ] No AWS account, credential, or resource is needed by the Vercel app.
- [ ] Existing AWS/EKS code remains unchanged and is validated separately.
- [ ] Vercel and Supabase usage/plan eligibility is confirmed by the project owner.

## Teardown

If the demo is no longer needed, remove the Vercel project and Supabase project from their respective dashboards after exporting any data that must be retained. Removing only this folder does not delete provider resources.
