// The window. All correctness rules live in Go; this draws the result and
// sends key presses back.

const go = () => window.go.main.App;
const $ = (id) => document.getElementById(id);

// Every backend call goes through here. A rejected promise used to mean a
// button that silently did nothing, with no way to tell a broken call from a
// no-op.
async function call(name, ...args) {
  try {
    const fn = go()?.[name];
    if (typeof fn !== "function") {
      hint(`${name} is not available in this build`, true);
      return null;
    }
    return await fn(...args);
  } catch (e) {
    hint(`${name} failed: ${e && e.message ? e.message : e}`, true);
    return null;
  }
}

let state = null;
let selected = -1;
let lastCount = 0;
let prompting = false; // a modal prompt is open
let hintTimer = null;

// ---------- rendering ----------

function render(s) {
  if (!s) return;
  state = s;

  const btn = $("startstop");
  btn.classList.toggle("running", s.active);
  btn.innerHTML = s.active ? "Stop" : 'Start <span class="glyph">&#9655;</span>';

  // Never fight the operator for a box they are typing in.
  if (document.activeElement !== $("row")) $("row").value = s.nextRow || 1;
  if (document.activeElement !== $("column")) $("column").value = s.nextColumn || 1;
  if (s.active) {
    if (document.activeElement !== $("rack")) $("rack").value = s.rack;
    $("can").value = s.can;
  }
  $("can").disabled = s.active;
  $("rack").disabled = s.active;
  $("row").disabled = !s.active;
  $("column").disabled = !s.active;

  $("undo").disabled = !s.canUndo;
  $("redo").disabled = !s.canRedo;
  // Exporting is what you do when the walking is done, so it unlocks on Stop.
  $("export").disabled = s.active || !s.hasSession || s.entries.length === 0;
  $("export").title = s.active ? "Press Stop to export" : "Export this rack as CSV";

  $("status-text").textContent =
    `${s.recorded} Device${s.recorded === 1 ? "" : "s"} Connected` +
    (s.hasSession ? `  ·  ${s.can} rack ${s.rack}  ·  ${s.entries.length} of ${s.positions} positions` : "") +
    (s.hasSession && !s.active ? "  ·  stopped" : "");

  if (s.error) hint(s.error, true);
  else if (s.copied) hint(`copied ${s.copied}`);
  else if (!s.listening) hint("not listening — no ports could be opened", true);
  else if (s.exported) hint(`exported ${s.exported}`);

  renderRows(s);
}

function renderRows(s) {
  const body = $("rows");
  const grew = s.entries.length > lastCount;
  lastCount = s.entries.length;

  $("empty").style.display = s.entries.length > 0 ? "none" : "";
  body.innerHTML = "";

  s.entries.forEach((e, i) => {
    const tr = document.createElement("tr");
    tr.className = (i === selected ? "sel " : "") +
                   (grew && i === s.entries.length - 1 ? "fresh" : "");
    tr.onclick = () => { selected = i; renderRows(state); };
    tr.oncontextmenu = (ev) => {
      ev.preventDefault();
      selected = i;
      renderRows(state);
      // Label the copy items with the values they will actually put on the
      // clipboard, and disable them on a row that has none.
      $("ctx-ip").textContent = e.ip || "";
      $("ctx-mac").textContent = e.mac || "";
      $("ctx").querySelector('[data-act="copyip"]').disabled = !e.ip;
      $("ctx").querySelector('[data-act="copymac"]').disabled = !e.mac;
      openMenu($("ctx"), ev.clientX, ev.clientY);
    };
    // Double-clicking a cell copies just that cell, which is quicker than the
    // menu when you already know which one you want.
    tr.ondblclick = async (ev) => {
      const cell = ev.target.closest("td");
      if (!cell) return;
      if (cell.classList.contains("ip")) render(await call("CopyIP", i));
      else if (cell.classList.contains("mac")) render(await call("CopyMAC", i));
    };

    tr.innerHTML =
      `<td class="no">${i + 1}</td><td class="pos">${e.row}</td><td class="pos">${e.column}</td>` +
      (e.kind === "skipped"
        ? `<td class="skip" colspan="2">skipped</td>`
        : `<td class="mono ip">${esc(e.ip)}</td><td class="mono mac">${esc(e.mac)}</td>`) +
      `<td class="note" title="${esc(e.note)}">${esc(e.note)}</td>`;
    body.appendChild(tr);
  });

  if (grew && body.lastElementChild) {
    body.lastElementChild.scrollIntoView({ block: "nearest" });
  }
}

function hint(msg, warn) {
  const el = $("status-hint");
  el.textContent = msg;
  el.className = "status-hint" + (warn ? " warn" : "");
  clearTimeout(hintTimer);
  hintTimer = setTimeout(() => { el.textContent = ""; el.className = "status-hint"; }, 4000);
}

// Derived from the DOM rather than tracked in a flag: disabling an element
// while it holds focus does not reliably fire blur, and a stuck flag would
// silently disable every keyboard shortcut including undo.
function typingInABox() {
  const el = document.activeElement;
  return !!el && (el.tagName === "INPUT" || el.tagName === "SELECT" || el.isContentEditable);
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---------- menus ----------

function openMenu(menu, x, y) {
  closeMenus();
  menu.classList.remove("hidden");
  const r = menu.getBoundingClientRect();
  menu.style.left = Math.min(x, innerWidth - r.width - 8) + "px";
  menu.style.top = Math.min(y, innerHeight - r.height - 8) + "px";

  // Arm the dismiss listener on the next tick. Opening a menu is itself a
  // mouse event, and a right-click is followed by a synthetic click in some
  // environments — arming immediately would close the menu before it is seen.
  setTimeout(() => document.addEventListener("mousedown", dismiss), 0);
}

function dismiss(ev) {
  if (ev && ev.target.closest(".menu")) return; // let the item handle itself
  closeMenus();
}

function closeMenus() {
  document.removeEventListener("mousedown", dismiss);
  $("ctx").classList.add("hidden");
}

document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape") closeMenus();
});

$("ctx").onclick = async (ev) => {
  const act = ev.target.closest("button")?.dataset.act;
  if (!act || selected < 0) return;
  closeMenus();
  switch (act) {
    case "copyip": render(await call("CopyIP", selected)); break;
    case "copymac": render(await call("CopyMAC", selected)); break;
    case "mac": await editMAC(selected); break;
    case "pos": await editRowPosition(selected); break;
    case "note": await editNote(selected); break;
    case "insert": render(await call("InsertBlankAbove", selected)); break;
    case "delete": await deleteRow(); break;
  }
};

$("export").onclick = async () => {
  if (!$("export").disabled) render(await call("Export"));
};

$("undo").onclick = async () => render(await call("Undo"));
$("redo").onclick = async () => render(await call("Redo"));

// ---------- position boxes ----------
//
// These are the display and the control at once: they show where the next
// machine lands, and typing in them moves the walk without touching a row
// already recorded.

async function commitPosition() {
  if (!state?.active) return;
  const row = parseInt($("row").value, 10);
  const col = parseInt($("column").value, 10);
  if (!Number.isFinite(row) || !Number.isFinite(col)) return;
  if (row === state.nextRow && col === state.nextColumn) return;

  const s = await call("SetNextPosition", col, row);
  const bad = !!s.error;
  $("row").classList.toggle("bad", bad);
  $("column").classList.toggle("bad", bad);
  render(s);
}

for (const id of ["row", "column"]) {
  $(id).addEventListener("blur", () => commitPosition());
  $(id).addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") { ev.preventDefault(); $(id).blur(); }
  });
}

// ---------- keys ----------
//
// Space is the only key pressed regularly, so it is bound alone: no modifier,
// no mouse, nothing to hit accurately while holding a torch.

document.addEventListener("keydown", async (ev) => {
  if (prompting || typingInABox() || !state?.active) return;
  const rows = state?.entries?.length ?? 0;

  switch (ev.key) {
    case " ":
      ev.preventDefault();
      render(await call("Skip"));
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
      if (selected >= 0) { ev.preventDefault(); await deleteRow(); }
      break;

    case "Enter":
      if (selected >= 0) { ev.preventDefault(); await editMAC(selected); }
      break;

    case "i": case "I":
      if (selected >= 0) { ev.preventDefault(); render(await call("InsertBlankAbove", selected)); }
      break;

    case "n": case "N":
      if (selected >= 0) { ev.preventDefault(); await editNote(selected); }
      break;

    case "p": case "P":
      if (selected >= 0) { ev.preventDefault(); await editRowPosition(selected); }
      break;

    case "z": case "Z":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await call("Undo")); }
      break;

    case "y": case "Y":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await call("Redo")); }
      break;

    case "e": case "E":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await call("Export")); }
      break;
  }
});

async function deleteRow() {
  render(await call("Delete", selected));
  selected = Math.min(selected, (state?.entries?.length ?? 1) - 1);
  renderRows(state);
}

async function editMAC(i) {
  prompting = true;
  try {
    const cur = state.entries[i]?.mac ?? "";
    const val = window.prompt("MAC address for this position (empty = skipped):", cur);
    if (val === null) return;
    render(await call("SetMAC", i, val.trim()));
  } finally { prompting = false; }
}

async function editNote(i) {
  prompting = true;
  try {
    const cur = state.entries[i]?.note ?? "";
    const val = window.prompt(
      "Note for this position — why it was skipped, or anything worth\n" +
      "finding again later. It is saved with the exported CSV.", cur);
    if (val === null) return;
    render(await call("SetNote", i, val));
  } finally { prompting = false; }
}

// Repositioning one recorded row, as opposed to moving the walk. Everything
// below it renumbers to follow.
async function editRowPosition(i) {
  prompting = true;
  try {
    const e = state.entries[i];
    const val = window.prompt(
      `Position for row ${i + 1}, as row/column. Rows below renumber to follow.\n` +
      `This rack is ${state.rows} rows by ${state.columns} columns.`,
      `${e.row}/${e.column}`);
    if (val === null) return;
    const m = val.match(/^\s*(\d+)\s*[\/,\s]\s*(\d+)\s*$/);
    if (!m) { hint(`"${val}" is not a row/column, e.g. 7/2`, true); return; }
    render(await call("SetPosition", i, parseInt(m[2], 10), parseInt(m[1], 10)));
  } finally { prompting = false; }
}

// ---------- start / stop ----------

async function boot() {
  const cans = await call("Cans");
  const sel = $("can");
  cans.forEach((c) => {
    const o = document.createElement("option");
    o.value = c; o.textContent = c;
    sel.appendChild(o);
  });

  $("startstop").onclick = async () => {
    if (state?.active) { render(await call("StopSession")); return; }
    const rack = parseInt($("rack").value, 10);
    if (!Number.isFinite(rack) || rack < 1) { hint("rack must be 1 or higher", true); return; }
    selected = -1;
    lastCount = 0;
    const s = await call("StartSession", sel.value, rack);
    render(s);
    if (!s.error) {
      const row = parseInt($("row").value, 10);
      const col = parseInt($("column").value, 10);
      if (row > 1 || col > 1) render(await call("SetNextPosition", col, row));
    }
  };

  runtime.EventsOn("captured", async () => render(await call("State")));
  runtime.EventsOn("rejected", async (msg) => { hint(msg, true); render(await call("State")); });
  runtime.EventsOn("notice", (msg) => hint(msg, true));

  render(await call("State"));
}

window.addEventListener("DOMContentLoaded", boot);
