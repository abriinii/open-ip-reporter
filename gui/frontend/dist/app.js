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
  $("export").disabled = s.active || !s.hasSession || (s.entries || []).length === 0;
  $("export").title = s.active ? "Press Stop to export" : "Export this rack as CSV";

  $("status-text").textContent =
    `${s.recorded} Device${s.recorded === 1 ? "" : "s"} Connected` +
    (s.hasSession ? `  ·  ${s.can} rack ${s.rack}  ·  ${(s.entries || []).length} of ${s.positions} positions` : "") +
    (s.hasSession && !s.active ? "  ·  stopped" : "");

  if (s.error) hint(s.error, true);
  else if (s.copied) hint(`copied ${s.copied}`);
  else if (!s.listening) hint("not listening: no ports could be opened", true);
  else if (s.exported) hint(`exported ${s.exported}`);

  applyUpdateState(s.updateState, s);
  setVersionLine();
  renderRows(s);
}

function renderRows(s) {
  const body = $("rows");
  // Belt and braces: Go now always sends a slice, but a null here used to
  // throw and take the whole of startup down with it.
  const entries = s.entries || [];
  const grew = entries.length > lastCount;
  lastCount = entries.length;

  $("empty").style.display = entries.length > 0 ? "none" : "";
  body.innerHTML = "";

  entries.forEach((e, i) => {
    const tr = document.createElement("tr");
    tr.className = (i === selected ? "sel " : "") +
                   (grew && i === entries.length - 1 ? "fresh" : "");
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
  const val = await ask({
    title: "MAC address",
    hint: "Leave it empty to make this a skipped position.",
    value: state.entries[i]?.mac ?? "",
    placeholder: "aa:bb:cc:dd:ee:ff",
  });
  if (val === null) return;
  render(await call("SetMAC", i, val.trim()));
}

// ask replaces window.prompt, which WKWebView does not implement, so on macOS
// every prompt returned nothing and the menu item appeared to do nothing at
// all. WebView2 does implement it, but frames it as "wails.localhost says".
//
// Resolves to the typed string, or null if cancelled.
function ask({ title, hint, value, placeholder }) {
  return new Promise((resolve) => {
    const input = $("ask-input");
    $("ask-title").textContent = title;
    $("ask-hint").textContent = hint || "";
    $("ask-error").textContent = "";
    input.value = value || "";
    input.placeholder = placeholder || "";

    $("askdlg").classList.remove("hidden");
    prompting = true;
    input.focus();
    input.select();

    const done = (result) => {
      $("askdlg").classList.add("hidden");
      prompting = false;
      input.onkeydown = null;
      $("ask-ok").onclick = null;
      $("ask-cancel").onclick = null;
      resolve(result);
    };

    $("ask-ok").onclick = () => done(input.value);
    $("ask-cancel").onclick = () => done(null);
    input.onkeydown = (ev) => {
      if (ev.key === "Enter") { ev.preventDefault(); done(input.value); }
      if (ev.key === "Escape") { ev.preventDefault(); done(null); }
    };
  });
}

async function editNote(i) {
  const val = await ask({
    title: "Note",
    hint: "Saved with the exported CSV.",
    value: state.entries[i]?.note ?? "",
    placeholder: "wont ip report",
  });
  if (val === null) return;
  render(await call("SetNote", i, val));
}

// Repositioning one recorded row, as opposed to moving the walk. Everything
// below it renumbers to follow.
async function editRowPosition(i) {
  const e = state.entries[i];
  const val = await ask({
    title: "Position",
    hint: `Row and column, like 7/2. Rows below renumber to follow.`,
    value: `${e.row}/${e.column}`,
    placeholder: "7/2",
  });
  if (val === null) return;

  const m = val.match(/^\s*(\d+)\s*[\/,\s]\s*(\d+)\s*$/);
  if (!m) { hint(`"${val}" is not a row and column, like 7/2`, true); return; }
  render(await call("SetPosition", i, parseInt(m[2], 10), parseInt(m[1], 10)));
}

// ---------- can editor ----------
//
// Site setup, touched once. Kept in a dialog so none of it is in the way while
// walking a rack.

let draft = [];

function renderCans() {
  const body = $("cansrows");
  body.innerHTML = "";
  $("cans-empty").classList.toggle("hidden", draft.length > 0);

  draft.forEach((c, i) => {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td><div class="can-cell">` +
        `<input data-i="${i}" value="${esc(c.name)}" placeholder="e.g. B1">` +
        `<button class="size" data-size="${i}" title="Rack size">${c.rows || 0}&#215;${c.columns || 0}</button>` +
      `</div></td>` +
      `<td class="del"><button class="rm" data-rm="${i}" title="Remove">&times;</button></td>`;
    body.appendChild(tr);
  });
}

$("cansrows").addEventListener("input", (ev) => {
  const i = ev.target.dataset.i;
  if (i !== undefined) draft[i].name = ev.target.value;
});

$("cansrows").addEventListener("click", (ev) => {
  const btn = ev.target.closest("button");
  if (!btn) return;
  if (btn.dataset.rm !== undefined) { draft.splice(Number(btn.dataset.rm), 1); renderCans(); }
  if (btn.dataset.size !== undefined) editCanSize(Number(btn.dataset.size));
});

// Rack size sits behind the name rather than in its own columns of spinners.
// It is set once per can and then never looked at again, so it does not earn
// a column beside the thing that is actually read.
async function editCanSize(i) {
  const c = draft[i];
  const val = await ask({
    title: c.name ? `Rack size for ${c.name}` : "Rack size",
    hint: "Rows and columns in one rack, like 10x5.",
    value: `${c.rows || 10}x${c.columns || 5}`,
    placeholder: "10x5",
  });
  if (val === null) return;

  const m = val.match(/^\s*(\d+)\s*[x×,\s]\s*(\d+)\s*$/i);
  if (!m) { $("cans-error").textContent = `"${val}" is not rows and columns, like 10x5`; return; }
  draft[i].rows = parseInt(m[1], 10);
  draft[i].columns = parseInt(m[2], 10);
  $("cans-error").textContent = "";
  renderCans();
}

$("addcan").onclick = () => {
  draft.push({ name: "", rows: 10, columns: 5 });
  renderCans();
  const inputs = $("cansrows").querySelectorAll("input");
  inputs[inputs.length - 1]?.focus();
};

$("importcans").onclick = async () => {
  const st = await call("ImportLayout");
  if (!st) return;
  if (st.error) { $("cans-error").textContent = st.error; return; }
  draft = ((await call("Layout")) || []).map((c) => ({ ...c }));
  $("cans-error").textContent = "";
  renderCans();
  await refreshCans();
};

$("examplecans").onclick = async () => {
  draft = ((await call("ExampleLayout")) || []).map((c) => ({ ...c }));
  renderCans();
};

async function openCans() {
  draft = ((await call("Layout")) || []).map((c) => ({ ...c }));
  $("cans-error").textContent = "";
  renderCans();
  $("cansdlg").classList.remove("hidden");
  prompting = true; // the walking shortcuts must not fire behind the dialog
}

function closeCans() {
  $("cansdlg").classList.add("hidden");
  prompting = false;
}

$("showfiles").onclick = (ev) => { ev.preventDefault(); call("OpenDataFolder"); };
$("editcans").onclick = openCans;
$("cans-cancel").onclick = closeCans;
$("cans-save").onclick = async () => {
  const s = await call("SaveLayout", draft);
  if (!s) return;
  if (s.error) { $("cans-error").textContent = s.error; return; }
  closeCans();
  await refreshCans();
  render(s);
};
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape" && !$("cansdlg").classList.contains("hidden")) closeCans();
});

// Rebuild the dropdown, keeping the current pick if it still exists.
async function refreshCans() {
  const sel = $("can");
  const previous = sel.value;
  const cans = (await call("Cans")) || [];
  sel.innerHTML = "";
  cans.forEach((c) => {
    const o = document.createElement("option");
    o.value = c; o.textContent = c;
    sel.appendChild(o);
  });
  if (cans.includes(previous)) sel.value = previous;
}

// ---------- launch update check ----------
//
// The check itself takes a fraction of a second on a good connection, which
// would flash past unread. It is held on screen for a moment so that "we
// looked, and here is the answer" is something the operator actually sees.

const TOAST_MIN_MS = 1100;
let checkStarted = 0;
let updateState = { state: "" };

// State carries both the running version and the newest published one. The
// update dialog must always take the latter: reading state.version here put
// the running version in a heading that said it was available.
function latestFromState() {
  return { version: state?.latestVersion, notes: state?.latestNotes };
}

function setVersionLine() {
  const v = state?.version && state.version !== "dev" ? state.version : "dev build";
  const el = $("version-line");
  switch (updateState.state) {
    case "checking":
      el.innerHTML = `${esc(v)} · checking…`;
      break;
    case "current": {
      // Clickable when notes came back with the check, so the notes for the
      // version in front of you can be read without falling behind first.
      const notes = state?.latestVersion;
      el.innerHTML = notes
        ? `${esc(v)} · <span class="stale ok" id="version-notes">up to date</span>`
        : `${esc(v)} · <span class="ok">up to date</span>`;
      if (notes) $("version-notes").onclick = () => showNotes();
      break;
    }
    case "available":
      el.innerHTML = `${esc(v)} · <span class="stale" id="version-update">${esc(state?.latestVersion || "update")} available</span>`;
      $("version-update").onclick = () => showUpdate(latestFromState());
      break;
    case "unreachable":
      el.innerHTML = `${esc(v)} · offline`;
      break;
    default:
      el.textContent = v;
  }
}

function toast(text, kind, holdMs) {
  const el = $("toast");
  $("toast-text").textContent = text;
  $("toast-spin").className = "spin" + (kind ? " " + kind : "");
  el.classList.remove("hidden", "out");
  if (holdMs) {
    setTimeout(() => {
      el.classList.add("out");
      setTimeout(() => el.classList.add("hidden"), 400);
    }, holdMs);
  }
}

// Resolve no sooner than TOAST_MIN_MS after the spinner appeared.
function afterMinimum(fn) {
  const waited = Date.now() - checkStarted;
  setTimeout(fn, Math.max(0, TOAST_MIN_MS - waited));
}

// Repeats of the same state are ignored so an ordinary re-render cannot
// restart the toast.
let lastAppliedState = "";

function applyUpdateState(stateName, s) {
  if (!stateName || stateName === lastAppliedState) return;
  lastAppliedState = stateName;
  onUpdateStatus({ ...(s || {}), state: stateName });
}

function onUpdateStatus(s) {
  updateState = s || { state: "" };
  switch (s.state) {
    case "checking":
      checkStarted = Date.now();
      toast("Checking for updates…");
      setVersionLine();
      break;

    case "current":
      afterMinimum(() => { toast("You're up to date", "done", 2200); setVersionLine(); });
      break;

    case "unreachable":
      // Standing in a can there is no route to the internet. Say so plainly
      // rather than implying something is broken.
      afterMinimum(() => { toast("No connection, could not check", "warn", 2600); setVersionLine(); });
      break;

    case "available":
      afterMinimum(() => {
        $("toast").classList.add("hidden");
        setVersionLine();
        showUpdate(latestFromState());
      });
      break;

    case "downloading": setUpdateBusy("Downloading…"); break;
    case "installing":  setUpdateBusy("Installing…"); break;
    case "restarting":  setUpdateBusy("Restarting…"); break;

    default: // "off" or "dev": nothing was checked, so show nothing
      $("toast").classList.add("hidden");
      setVersionLine();
  }
}

// One call, one answer. It blocks in Go until GitHub replies or gives up, so
// there is no event to miss and no polling to stop too early.
async function runUpdateCheck() {
  onUpdateStatus({ state: "checking" });
  const s = await call("CheckForUpdate");
  if (!s) { $("toast").classList.add("hidden"); return; }
  state = s;
  applyUpdateState(s.updateState, s);
  setVersionLine();
}

// ---------- update notice ----------
//
// Shown only when a newer release actually exists, which is rare. The app
// never downloads or installs anything; this is a notification and a link.

// The notes for the version already running: same dialog, nothing to install.
function showNotes() {
  const v = state?.latestVersion || state?.version || "";
  $("up-title").textContent = `What's new in ${v}`;
  $("up-current").textContent = "You are running this version already.";
  $("up-notes").textContent = state?.latestNotes || "No release notes were published for this version.";
  $("up-never").parentElement.classList.add("hidden");
  $("up-open").classList.add("hidden");
  $("up-page").classList.add("hidden");
  $("up-later").textContent = "Close";
  $("updatedlg").classList.remove("hidden");
  prompting = true;
}

async function showUpdate(rel) {
  rel = rel || latestFromState();
  if (!rel.version) {
    // The details may still be on their way. Ask once rather than doing
    // nothing, which is what made the button look broken.
    const s = await call("State");
    if (s) { state = s; rel = latestFromState(); }
    if (!rel.version) { hint("still checking, try again in a moment"); return; }
  }
  $("up-title").textContent = `Version ${rel.version} is available`;
  $("up-never").parentElement.classList.remove("hidden");
  $("up-open").classList.remove("hidden");
  $("up-page").classList.remove("hidden");
  setUpdateBusy(null);
  $("up-later").textContent = "Later";
  $("up-current").textContent =
    `You are running ${state?.version || "an older version"}. ` +
    `Update now downloads it, checks it against its published checksum, ` +
    `replaces this copy and restarts.`;
  $("up-notes").textContent = rel.notes || "No release notes were published for this version.";
  $("up-never").checked = false;
  $("updatedlg").classList.remove("hidden");
  prompting = true;
}

function closeUpdate() {
  if ($("up-never").checked) call("SetCheckForUpdates", false);
  $("updatedlg").classList.add("hidden");
  prompting = false;
}

$("up-later").onclick = closeUpdate;
$("up-page").onclick = () => call("OpenReleasePage");
$("up-open").onclick = async () => {
  // The dialog stays open and reports progress. Closing it would leave the
  // program replacing itself with nothing on screen saying so.
  setUpdateBusy("Downloading…");
  const s = await call("InstallUpdate");
  if (s && s.updateError) {
    setUpdateBusy(null);
    $("up-notes").textContent = "Update failed: " + s.updateError +
      "\n\nThe running copy has not been touched. Use the release page instead.";
  }
};

function setUpdateBusy(label) {
  const busy = label !== null;
  $("up-open").disabled = busy;
  $("up-page").disabled = busy;
  $("up-later").disabled = busy;
  $("up-never").disabled = busy;
  $("up-open").textContent = busy ? label : "Update now";
}
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape" && !$("updatedlg").classList.contains("hidden")) closeUpdate();
});

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

  runtime.EventsOn("update-status", (st) =>
    applyUpdateState(typeof st === "string" ? st : st && st.state, state));
  runtime.EventsOn("captured", async () => render(await call("State")));
  runtime.EventsOn("rejected", async (msg) => { hint(msg, true); render(await call("State")); });
  runtime.EventsOn("notice", (msg) => hint(msg, true));

  render(await call("State"));
  setVersionLine();

  runUpdateCheck();
}

window.addEventListener("DOMContentLoaded", boot);
