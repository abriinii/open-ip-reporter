// The window. All correctness rules live in Go; this draws the result and
// sends key presses back.

const go = () => window.go.main.App;
const $ = (id) => document.getElementById(id);

let state = null;
let selected = -1;
let lastCount = 0;
let editing = false;   // suppress global keys while a text box has focus
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
  // The shape of a rack is fixed once a walk begins: changing it underneath
  // recorded rows would move machines that are already placed.
  $("geom-rows").disabled = s.active;
  $("geom-cols").disabled = s.active;
  if (s.active) { $("geom-rows").value = s.rows; $("geom-cols").value = s.columns; }
  $("row").disabled = !s.active;
  $("column").disabled = !s.active;

  $("undo").disabled = !s.canUndo;
  $("redo").disabled = !s.canRedo;
  $("export").disabled = !s.active || s.entries.length === 0;

  $("status-text").textContent =
    `${s.recorded} Device${s.recorded === 1 ? "" : "s"} Connected` +
    (s.active ? `  ·  ${s.can} rack ${s.rack}  ·  ${s.entries.length} of ${s.positions} positions` : "");

  if (s.error) hint(s.error, true);
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
      openMenu($("ctx"), ev.clientX, ev.clientY);
    };

    tr.innerHTML =
      `<td class="no">${i + 1}</td><td class="pos">${e.row}</td><td class="pos">${e.column}</td>` +
      (e.kind === "skipped"
        ? `<td class="skip" colspan="2">skipped</td>`
        : `<td class="mono">${esc(e.ip)}</td><td class="mono">${esc(e.mac)}</td>`);
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
    case "mac": await editMAC(selected); break;
    case "pos": await editRowPosition(selected); break;
    case "insert": render(await go().InsertBlankAbove(selected)); break;
    case "delete": await deleteRow(); break;
  }
};

$("export").onclick = async () => {
  if (!$("export").disabled) render(await go().Export());
};

$("undo").onclick = async () => render(await go().Undo());
$("redo").onclick = async () => render(await go().Redo());

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

  const s = await go().SetNextPosition(col, row);
  const bad = !!s.error;
  $("row").classList.toggle("bad", bad);
  $("column").classList.toggle("bad", bad);
  render(s);
}

for (const id of ["row", "column"]) {
  $(id).addEventListener("focus", () => { editing = true; });
  $(id).addEventListener("blur", () => { editing = false; commitPosition(); });
  $(id).addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") { ev.preventDefault(); $(id).blur(); }
  });
}
for (const id of ["rack", "can", "geom-rows", "geom-cols"]) {
  $(id).addEventListener("focus", () => { editing = true; });
  $(id).addEventListener("blur", () => { editing = false; });
}

// ---------- keys ----------
//
// Space is the only key pressed regularly, so it is bound alone: no modifier,
// no mouse, nothing to hit accurately while holding a torch.

document.addEventListener("keydown", async (ev) => {
  if (editing || !state?.active) return;
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
      if (selected >= 0) { ev.preventDefault(); await deleteRow(); }
      break;

    case "Enter":
      if (selected >= 0) { ev.preventDefault(); await editMAC(selected); }
      break;

    case "i": case "I":
      if (selected >= 0) { ev.preventDefault(); render(await go().InsertBlankAbove(selected)); }
      break;

    case "p": case "P":
      if (selected >= 0) { ev.preventDefault(); await editRowPosition(selected); }
      break;

    case "z": case "Z":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await go().Undo()); }
      break;

    case "y": case "Y":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await go().Redo()); }
      break;

    case "e": case "E":
      if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); render(await go().Export()); }
      break;
  }
});

async function deleteRow() {
  render(await go().Delete(selected));
  selected = Math.min(selected, (state?.entries?.length ?? 1) - 1);
  renderRows(state);
}

async function editMAC(i) {
  editing = true;
  try {
    const cur = state.entries[i]?.mac ?? "";
    const val = window.prompt("MAC address for this position (empty = skipped):", cur);
    if (val === null) return;
    render(await go().SetMAC(i, val.trim()));
  } finally { editing = false; }
}

// Repositioning one recorded row, as opposed to moving the walk. Everything
// below it renumbers to follow.
async function editRowPosition(i) {
  editing = true;
  try {
    const e = state.entries[i];
    const val = window.prompt(
      `Position for row ${i + 1}, as row/column. Rows below renumber to follow.\n` +
      `This rack is ${state.rows} rows by ${state.columns} columns.`,
      `${e.row}/${e.column}`);
    if (val === null) return;
    const m = val.match(/^\s*(\d+)\s*[\/,\s]\s*(\d+)\s*$/);
    if (!m) { hint(`"${val}" is not a row/column, e.g. 7/2`, true); return; }
    render(await go().SetPosition(i, parseInt(m[2], 10), parseInt(m[1], 10)));
  } finally { editing = false; }
}

// ---------- start / stop ----------

async function boot() {
  const cans = await go().Cans();
  const sel = $("can");
  cans.forEach((c) => {
    const o = document.createElement("option");
    o.value = c; o.textContent = c;
    sel.appendChild(o);
  });

  $("startstop").onclick = async () => {
    if (state?.active) { render(await go().StopSession()); return; }
    const rack = parseInt($("rack").value, 10);
    if (!Number.isFinite(rack) || rack < 1) { hint("rack must be 1 or higher", true); return; }
    selected = -1;
    lastCount = 0;
    const rows = parseInt($("geom-rows").value, 10);
    const cols = parseInt($("geom-cols").value, 10);
    const s = await go().StartSession(sel.value, rack, rows, cols);
    render(s);
    if (!s.error) {
      const row = parseInt($("row").value, 10);
      const col = parseInt($("column").value, 10);
      if (row > 1 || col > 1) render(await go().SetNextPosition(col, row));
    }
  };

  // Prefill the size boxes from the can's usual shape, but leave them editable.
  sel.onchange = async () => {
    const g = await go().GeometryFor(sel.value);
    if (g && g.Rows) { $("geom-rows").value = g.Rows; $("geom-cols").value = g.Columns; }
  };

  runtime.EventsOn("captured", async () => render(await go().State()));
  runtime.EventsOn("rejected", async (msg) => { hint(msg, true); render(await go().State()); });
  runtime.EventsOn("notice", (msg) => hint(msg, true));

  render(await go().State());
}

window.addEventListener("DOMContentLoaded", boot);
