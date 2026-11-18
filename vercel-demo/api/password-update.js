"use strict";

(() => {
  const form = document.querySelector("#password-form");
  const password = document.querySelector("#password");
  const confirmPassword = document.querySelector("#confirm-password");
  const submit = document.querySelector("#submit");
  const status = document.querySelector("#status");
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const accessToken = fragment.get("access_token");
  const type = fragment.get("type");

  window.history.replaceState(null, "", window.location.pathname);

  if (!accessToken || !["recovery", "invite", "signup", "magiclink", "email_change"].includes(type)) {
    status.textContent = "This password link is invalid or expired. Request a new one and try again.";
    return;
  }

  form.hidden = false;
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (password.value !== confirmPassword.value) {
      status.textContent = "The passwords do not match.";
      return;
    }

    submit.disabled = true;
    status.textContent = "Updating password...";
    try {
      const response = await fetch("/admin/api/password-update", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ access_token: accessToken, password: password.value }),
      });
      if (!response.ok) {
        status.textContent = await response.text();
        return;
      }
      form.reset();
      status.textContent = "Password updated. You can now sign in.";
    } catch {
      status.textContent = "Unable to update password. Please try again.";
    } finally {
      submit.disabled = false;
    }
  });
})();
