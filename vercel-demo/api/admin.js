const status = document.getElementById("status");
const loadStatus = document.getElementById("load-status");
const loadButton = document.getElementById("load-button");
let canRunSimulation = false;

async function requestJSON(url, options = {}) {
  const response = await fetch(url, { cache: "no-store", ...options });
  if (response.status === 401) {
    window.location.assign("/admin/login");
    throw new Error("Sign-in required");
  }
  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(message || `Request failed (${response.status})`);
  }
  if (response.status === 204) return null;
  return response.json();
}

function drawChart(id, values) {
  const path = document.querySelector(`#${id} path`);
  if (!values.length) {
    path.setAttribute("d", "");
    return;
  }
  const max = Math.max(1, ...values);
  const d = values.map((value, index) => {
    const x = values.length < 2 ? 300 : index * 600 / (values.length - 1);
    const y = 103 - value / max * 92;
    return `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  path.setAttribute("d", d);
}

async function refreshMetrics() {
  const data = await requestJSON("/admin/api/metrics");
  const points = data.points || [];
  const sum = key => points.reduce((total, point) => total + point[key], 0);
  const count = sum("requests");
  const latency = points.reduce((total, point) => total + point.latency_ms * point.requests, 0);
  document.getElementById("request-total").textContent = count.toLocaleString();
  document.getElementById("error-total").textContent = sum("errors").toLocaleString();
  document.getElementById("latency-average").textContent = count ? `${(latency / count).toFixed(1)} ms` : "No data";
  document.getElementById("activity-total").textContent =
    `${sum("comments")} / ${sum("photo_likes")} / ${sum("comment_likes")}`;
  drawChart("requests-chart", points.map(point => point.requests));
  drawChart("latency-chart", points.map(point => point.latency_ms));
  document.getElementById("sim-cpu").textContent = `${data.simulation.cpu_percent}%`;
  document.getElementById("sim-memory").textContent = `${data.simulation.memory_mb} MB`;
  document.getElementById("sim-replicas").textContent = data.simulation.replicas;
  document.getElementById("sim-autoscaling").textContent = data.simulation.autoscaling;
  document.getElementById("sim-alerts").textContent = data.simulation.alerts;
  document.getElementById("sim-users").textContent = data.simulation.virtual_users;
  document.getElementById("sim-rps").textContent = data.simulation.requests_per_second;
  document.getElementById("sim-cpu").setAttribute("aria-label", `${data.simulation.cpu_percent} percent simulated CPU`);
  const simulatedPoints = data.simulation_history || [];
  drawChart("sim-rps-chart", simulatedPoints.map(point => point.requests_per_second));
  drawChart("sim-cpu-chart", simulatedPoints.map(point => point.cpu_percent));
  drawChart("sim-replicas-chart", simulatedPoints.map(point => point.replicas));
  document.getElementById("sim-autoscaling").setAttribute("aria-live", "polite");
  document.getElementById("updated-at").textContent =
    `Updated ${new Date(data.collected_at).toLocaleTimeString()}. Real source: persisted application request buckets.`;
}

async function refreshLoadHistory() {
  const runs = await requestJSON("/admin/api/load-test");
  const container = document.getElementById("load-history");
  container.replaceChildren();
  if (!runs.length) {
    container.textContent = "No simulation runs recorded.";
    loadButton.disabled = !canRunSimulation;
    return;
  }
  const list = document.createElement("ul");
  for (const run of runs) {
    const item = document.createElement("li");
    const date = new Date(run.started_at).toLocaleString();
    item.textContent = `${date} — ${run.actor_username}: ${run.state}; ${run.simulated_vus} simulated users for ${Math.round(run.duration_ms / 1000)} seconds (${run.result})`;
    list.append(item);
  }
  container.append(list);
  const activeRun = runs.find(run => run.state === "running");
  const latestRun = runs[0];
  if (activeRun) {
    loadButton.disabled = true;
    loadStatus.textContent = `Simulation in progress: ${activeRun.seconds_remaining}s remaining. No synthetic workload is being sent; routine dashboard refreshes are real requests.`;
  } else if (latestRun.cooldown_remaining > 0) {
    loadButton.disabled = true;
    loadStatus.textContent = `Simulation complete. Re-run available in ${latestRun.cooldown_remaining}s.`;
  } else {
    loadButton.disabled = !canRunSimulation;
    if (loadStatus.textContent.includes("in progress") || loadStatus.textContent.includes("complete. Re-run")) {
      loadStatus.textContent = "Simulation complete. Infrastructure values are returning to baseline.";
    }
  }
}

async function runSimulation() {
  loadButton.disabled = true;
  loadStatus.textContent = "Starting a 90-second simulated load scenario. No synthetic requests will be sent to Vercel…";
  try {
    await requestJSON("/admin/api/load-test", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    await Promise.all([refreshMetrics(), refreshLoadHistory()]);
  } catch (error) {
    loadStatus.textContent = error.message;
    try {
      await refreshLoadHistory();
    } catch {
      loadButton.disabled = false;
    }
  }
}

async function changeAccess(username, action, enabled) {
  await requestJSON("/admin/api/access", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, action, enabled })
  });
}

async function refreshAdmins() {
  const data = await requestJSON("/admin/api/access");
  const container = document.getElementById("admins-list");
  container.replaceChildren();
  for (const admin of data) {
    const row = document.createElement("div");
    row.className = "admin-row";
    const name = document.createElement("strong");
    name.textContent = admin.username;
    row.append(name);
    row.append(makeToggle(admin, "enabled", "account", "Account enabled"));
    row.append(makeToggle(admin, "can_load_test", "load_test", "May run simulation"));
    const reset = document.createElement("button");
    reset.className = "secondary";
    reset.type = "button";
    reset.textContent = "Send password reset";
    reset.addEventListener("click", async () => {
      reset.disabled = true;
      try {
        await requestJSON("/admin/api/access", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ username: admin.username, action: "password_reset" })
        });
        document.getElementById("access-status").textContent = `Supabase accepted the password-reset request for ${admin.username}; confirm that the email is delivered.`;
      } catch (error) {
        document.getElementById("access-status").textContent = error.message;
      } finally {
        reset.disabled = false;
      }
    });
    row.append(reset);
    container.append(row);
  }
}

function makeToggle(admin, property, action, label) {
  const wrapper = document.createElement("label");
  wrapper.className = "permission-toggle";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = Boolean(admin[property]);
  input.setAttribute("aria-label", `${label} for ${admin.username}`);
  input.addEventListener("change", async () => {
    input.disabled = true;
    try {
      await changeAccess(admin.username, action, input.checked);
    } catch (error) {
      input.checked = !input.checked;
      document.getElementById("access-status").textContent = error.message;
    } finally {
      input.disabled = false;
    }
  });
  wrapper.append(input, document.createTextNode(label));
  return wrapper;
}

async function initializeAdmin() {
  const session = await requestJSON("/admin/api/session");
  document.getElementById("signed-in").textContent = `Signed in as ${session.username} (${session.role})`;
  canRunSimulation = Boolean(session.can_load_test);
  loadButton.disabled = !canRunSimulation;
  if (!session.can_load_test) loadStatus.textContent = "You do not have permission to run the simulation.";
  if (session.is_owner) {
    const button = document.getElementById("access-button");
    button.hidden = false;
    button.addEventListener("click", async () => {
      const panel = document.getElementById("access-panel");
      panel.hidden = !panel.hidden;
      if (panel.hidden) return;
      try {
        await refreshAdmins();
      } catch (error) {
        document.getElementById("access-status").textContent = error.message;
      }
    });
  }
  await Promise.all([refreshMetrics(), refreshLoadHistory()]);
  window.setInterval(async () => {
    try {
      await Promise.all([refreshMetrics(), refreshLoadHistory()]);
      status.textContent = "";
    } catch (error) {
      status.textContent = error.message;
    }
  }, 10000);
}

const loginForm = document.getElementById("login-form");
if (loginForm) {
  loginForm.addEventListener("submit", async event => {
    event.preventDefault();
    const button = document.getElementById("submit");
    const message = document.getElementById("status");
    button.disabled = true;
    message.textContent = "Signing in…";
    try {
      const response = await fetch("/admin/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: document.getElementById("email").value.trim(),
          password: document.getElementById("password").value
        })
      });
      if (!response.ok) throw new Error((await response.text()).trim() || "Sign-in failed.");
      document.getElementById("password").value = "";
      window.location.assign("/admin");
    } catch (error) {
      message.textContent = error.message;
    } finally {
      button.disabled = false;
    }
  });
} else {
  document.getElementById("refresh-button").addEventListener("click", async () => {
    status.textContent = "";
    try {
      await Promise.all([refreshMetrics(), refreshLoadHistory()]);
    } catch (error) {
      status.textContent = error.message;
    }
  });
  loadButton.addEventListener("click", runSimulation);
  document.getElementById("logout-button").addEventListener("click", async () => {
    try {
      await requestJSON("/admin/api/logout", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
      window.location.assign("/admin/login");
    } catch (error) {
      status.textContent = error.message;
    }
  });
  initializeAdmin().catch(error => {
    status.textContent = error.message;
  });
}
