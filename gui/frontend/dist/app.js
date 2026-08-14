// The window. All correctness rules live in Go; this draws the result and
// sends key presses back.

const go = () => window.go.main.App;
const $ = (id) => document.getElementById(id);

let state = null;
let selected = -1;   // highlighted row, -1 for none
let lastCount = 0;   // so only genuinely new rows flash
let editing = false; // suppress global keys while a prompt is open
let toastTimer = null;

// ---------- rendering ----------

function render(s) {
  if (!s) return;
  state = s;

  $("listen-dot").className = "dot " + (s.listening ? "on" : "off");
  $("listen-dot").title = s.listening
    ? `listening on ${s.boundPorts} ports`
    : "not listening";

  if (!s.active) {
    $("app").classList.add("hidden");
    $("setup").classList.remove("hidden");
    return;
  }
  $("setup").classList.add("hidden");
  $("app").classList.remove("hidden");

  $("loc").textContent = `${s.can} · rack ${s.rack}`;
  $("count").textContent = `${s.entries.length} of ${s.positions}`;

  if (s.full) {
    $("next-pos").textContent = "RACK DONE";
    $("next-pos").classList.add("done");
  } else {
    $("next-pos").textContent = s.nextLabel || "—";
    $("next-pos").classList.remove("done");
  }

  if (s.error) toast(s.error, "info");
  renderRows(s);
}

function renderRows(s) {
  const body = $("rows");
  const grew = s.entries.length > lastCount;
  lastCount = s.entries.length;

  $("empty").classList.toggle("hidden", s.entries.length > 0);
  body.innerHTML = "";

  s.entries.forEach((e, i) => {
    const tr = document.createElement("tr");
    tr.className = (i === selected ? "sel " : "") +
                   (grew && i === s.entries.length - 1 ? "fresh" : "");
    tr.onclick = () => { selected = i; renderRows(state); };

    tr.innerHTML = e.kind === "skipped"
      ? `<td class="pos">${e.label}</td><td class="skip">skipped</td><td class="time"></td>`
      : `<td class="pos">${e.label}</td><td>${esc(e.mac)}</td><td class="time">${esc(e.time)}</td>`;
    body.appendChild(tr);
  });

  if (grew && body.lastElementChild) {
    body.lastElementChild.scrollIntoView({ block: "nearest" });
  }
}

function toast(msg, kind) {
  const t = $("toast");
  t.textContent = msg;
  t.className = `toast show ${kind || "info"}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = "toast"; }, 3500);
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---------- keys ----------
//
// Space is the only key pressed regularly, so it is bound alone: no modifier,
// no mouse, nothing to hit accurately while holding a torch.

document.addEventListener("keydown", async (ev) => {
  if (editing) return;
  if ($("app").classList.contains("hidden")) return;

  const rows = state?.entries?.length ?? 0;

  switch (ev.key) {
    case " ":
      ev.preventDefault();
      render(await go().Skip());
      break;

    case "ArrowDown":
      ev.preventDefault();
      selected = rows === 0 ? -1 : Math.min(rows - 1, selected + 1);
      renderRows(state);
      break;

    case "ArrowUp":
      ev.preventDefault();
      selected = Math.max(0, selected - 1);
      renderRows(state);
      break;

    case "Delete":
    case "Backspace":
      if (selected >= 0) {
        ev.preventDefault();
        render(await go().Delete(selected));
        selected = Math.min(selected, (state?.entries?.length ?? 1) - 1);
        renderRows(state);
      }
      break;

    case "Enter":
      if (selected >= 0) { ev.preventDefault(); await editMAC(selected); }
      break;

    case "i": case "I":
      if (selected >= 0) { ev.preventDefault(); render(await go().InsertBlankAbove(selected)); }
      break;

    case "p": case "P":
      if (selected >= 0) { ev.preventDefault(); await editPosition(selected); }
      break;

    case "z": case "Z":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await go().Undo()); }
      break;

    case "y": case "Y":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await go().Redo()); }
      break;
  }
});

async function editMAC(i) {
  editing = true;
  try {
    const cur = state.entries[i]?.mac ?? "";
    const val = window.prompt("MAC for this position (empty = skipped):", cur);
    if (val === null) return;
    render(await go().SetMAC(i, val.trim()));
  } finally { editing = false; }
}

async function editPosition(i) {
  editing = true;
  try {
    const e = state.entries[i];
    const val = window.prompt(
      `Position as column/row. Rows below renumber to follow.\n` +
      `This rack is ${state.columns} columns by ${state.rows} rows.`,
      `${e.column}/${e.row}`);
    if (val === null) return;
    const m = val.match(/^\s*(\d+)\s*[\/,\s]\s*(\d+)\s*$/);
    if (!m) { toast(`"${val}" is not a column/row, e.g. 2/7`, "info"); return; }
    render(await go().SetPosition(i, parseInt(m[1], 10), parseInt(m[2], 10)));
  } finally { editing = false; }
}

// ---------- setup ----------

async function boot() {
  const cans = await go().Cans();
  const sel = $("can");
  cans.forEach((c) => {
    const o = document.createElement("option");
    o.value = c; o.textContent = c;
    sel.appendChild(o);
  });

  $("start").onclick = async () => {
    const rack = parseInt($("rack").value, 10);
    if (!Number.isFinite(rack) || rack < 1) {
      $("setup-error").textContent = "Rack must be 1 or higher.";
      return;
    }
    const s = await go().StartSession(sel.value, rack);
    $("setup-error").textContent = s.error || "";
    selected = -1;
    lastCount = 0;
    render(s);
  };

  $("change").onclick = () => {
    $("app").classList.add("hidden");
    $("setup").classList.remove("hidden");
  };

  $("more-keys").onclick = () => {
    toast("I = insert blank above · P = set position · Ctrl+Y = redo", "info");
  };

  runtime.EventsOn("captured", async () => render(await go().State()));
  // A refused duplicate: say where it already is, and leave the position alone.
  runtime.EventsOn("rejected", (msg) => toast(msg, "reject"));
  runtime.EventsOn("notice", (msg) => toast(msg, "info"));

  render(await go().State());
}

window.addEventListener("DOMContentLoaded", boot);
