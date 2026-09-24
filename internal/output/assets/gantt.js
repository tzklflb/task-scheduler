const data = JSON.parse(document.getElementById("schedule-data").textContent);

const escapeHTML = value => String(value).replace(/[&<>"']/g, char => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
})[char]);
const parseDate = value => { const [year, month, day] = value.split("-").map(Number); return new Date(year, month - 1, day); };
const isoDate = value => `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")}`;
const shortDate = value => value.slice(5).replace("-", "/");
const statusLabel = task => task.Status === "done" ? "完了" : task.Status === "in_progress" ? "着手中" : "予定";
const priorityLabel = task => ({
  core: "コア",
  release_required: "リリースまで",
  post_release: "リリース後"
})[task.DeliveryScope];
const periodLabel = task => `${task.Start}${task.StartsInAfternoon ? " 午後" : ""}〜${task.End}${task.EndsAtNoon ? " 午前" : ""}`;
const workerPalette = ["#2a78d6", "#1baf7a", "#eda100", "#008300", "#8250df"];
const workerColor = Object.fromEntries(data.Workers.map((worker, index) => [worker, workerPalette[index % workerPalette.length]]));

document.title = data.Title;
document.getElementById("title").textContent = data.Title;

const summary = [
  ["開始", data.Start],
  ["コア完了", data.CoreEnd],
  ["開発完了", data.CompletionWithBuffer],
  ["バッファ", data.BufferDates.length ? `${data.BufferDates.length} 日（${data.BufferDates.join("、")}）` : "0 日"],
  ["残工数", data.RemainingEffort.toFixed(1)]
];
if (data.AsOf) summary.splice(1, 0, ["基準日", data.AsOf]);
if (data.ReleaseDate) summary.push(["リリース", data.ReleaseDate]);
summary.push(["今日", isoDate(new Date())]);
document.getElementById("summary").innerHTML = summary.map(([name, value]) =>
  `<div><dt>${escapeHTML(name)}</dt><dd>${escapeHTML(value)}</dd></div>`
).join("");

const assumptions = document.getElementById("assumptions");
assumptions.innerHTML = data.Assumptions.length
  ? data.Assumptions.map(value => `<li>${escapeHTML(value)}</li>`).join("")
  : "<li>なし</li>";

const sprintGoals = document.getElementById("sprint-goals");
if (data.SprintGoals.length) {
  sprintGoals.innerHTML = `<table><thead><tr><th>スプリント</th><th>期間</th><th>ゴール</th></tr></thead><tbody>${data.SprintGoals.map(goal =>
    `<tr><td>S${goal.Number}</td><td class="date">${escapeHTML(shortDate(goal.Start))}〜${escapeHTML(shortDate(goal.End))}</td><td>${escapeHTML(goal.Goal)}</td></tr>`
  ).join("")}</tbody></table>`;
} else {
  sprintGoals.hidden = true;
}

const taskList = [...data.Tasks, ...data.ExcludedTasks];
document.getElementById("task-list").innerHTML = taskList.map(task => `<tr>
  <td>${escapeHTML(task.ID)}</td><td>${escapeHTML(task.Name)}</td><td>${escapeHTML(task.Assignee || "-")}</td>
  <td>${escapeHTML(task.Plan)}</td><td class="effort">${task.Effort.toFixed(1)}</td>
  <td>${escapeHTML(priorityLabel(task))}</td><td>${escapeHTML(statusLabel(task))}</td>
</tr>`).join("");

const PX = 22;
const LABEL_WIDTH = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--gantt-label-width")) || 330;
const holidays = new Set(data.Holidays);
const halfDays = new Set(data.HalfDays);
const bufferDates = new Set(data.BufferDates);
const today = isoDate(new Date());
const taskStart = data.Tasks.reduce((first, task) => !first || task.Start < first ? task.Start : first, data.Start);
const chartEndValues = [data.CompletionWithBuffer, data.CoreEnd, data.ReleaseDate, ...data.Tasks.map(task => task.End)].filter(Boolean);
const taskEnd = chartEndValues.reduce((last, value) => value > last ? value : last, data.Start);
const firstDate = parseDate(taskStart);
firstDate.setDate(firstDate.getDate() - 2);
const lastDate = parseDate(taskEnd);
lastDate.setDate(lastDate.getDate() + 3);
const visibleDates = [];
for (const date = new Date(firstDate); date <= lastDate; date.setDate(date.getDate() + 1)) {
  if (date.getDay() !== 0 && date.getDay() !== 6) visibleDates.push(isoDate(date));
}
const visibleDateCountBefore = value => {
  let lower = 0;
  let upper = visibleDates.length;
  while (lower < upper) {
    const middle = Math.floor((lower + upper) / 2);
    if (visibleDates[middle] < value) lower = middle + 1;
    else upper = middle;
  }
  return lower;
};
const visibleDateIndex = value => {
  const index = visibleDateCountBefore(value);
  return visibleDates[index] === value ? index : -1;
};
const boundaryPosition = value => LABEL_WIDTH + visibleDateCountBefore(value) * PX;
const taskStartPosition = (value, startsInAfternoon) => {
  const index = visibleDateIndex(value);
  return boundaryPosition(value) + (index >= 0 && startsInAfternoon ? PX / 2 : 0);
};
const taskEndPosition = (value, endsAtNoon) => {
  const index = visibleDateIndex(value);
  return index < 0 ? boundaryPosition(value) : LABEL_WIDTH + index * PX + (endsAtNoon ? PX / 2 : PX);
};
const gantt = document.getElementById("gantt");
gantt.style.width = `${LABEL_WIDTH + visibleDates.length * PX}px`;
const tasksByID = new Map(data.Tasks.map(task => [task.ID, task]));
const tooltip = document.getElementById("task-tooltip");
let activeTooltipBar;

const tooltipText = task => [
  `名前: ${task.Name}`,
  `計画: ${task.Plan}`,
  `期間: ${periodLabel(task)}`,
  `工数: ${task.Effort.toFixed(1)}`
].join("\n");

const positionTooltip = (bar, point) => {
  const anchor = point || bar.getBoundingClientRect();
  const anchorX = point ? point.clientX : anchor.left + anchor.width / 2;
  const anchorY = point ? point.clientY : anchor.bottom;
  const padding = 12;
  const gap = 12;
  const bounds = tooltip.getBoundingClientRect();
  const left = Math.max(padding, Math.min(anchorX + gap, innerWidth - bounds.width - padding));
  const below = anchorY + gap;
  const above = anchorY - bounds.height - gap;
  const top = Math.max(padding, Math.min(below + bounds.height > innerHeight - padding ? above : below, innerHeight - bounds.height - padding));
  tooltip.style.left = `${left}px`;
  tooltip.style.top = `${top}px`;
};

const showTooltip = (bar, point) => {
  const task = tasksByID.get(bar.dataset.taskId);
  if (!task) return;
  if (activeTooltipBar && activeTooltipBar !== bar) activeTooltipBar.removeAttribute("aria-describedby");
  activeTooltipBar = bar;
  tooltip.textContent = tooltipText(task);
  tooltip.hidden = false;
  bar.setAttribute("aria-describedby", "task-tooltip");
  positionTooltip(bar, point);
};

const hideTooltip = bar => {
  if (!activeTooltipBar || (bar && activeTooltipBar !== bar)) return;
  activeTooltipBar.removeAttribute("aria-describedby");
  activeTooltipBar = undefined;
  tooltip.hidden = true;
};

const milestones = [
  [data.CoreEnd, "コア完了"],
  [data.CompletionWithBuffer, "開発完了"],
  [data.ReleaseDate, "リリース"],
  [data.AsOf, "基準日"]
].filter(([date]) => date);
if (today >= isoDate(firstDate) && today <= isoDate(lastDate)) milestones.push([today, "今日", true]);
const milestoneGroups = new Map();
for (const [date, label] of milestones) {
  const left = boundaryPosition(date);
  const group = milestoneGroups.get(left) || { left, labels: [] };
  group.labels.push(label);
  milestoneGroups.set(left, group);
}
const milestoneBandHeight = Math.max(42, ...[...milestoneGroups.values()].map(group => group.labels.join("・").length * 12 + 8));
gantt.style.setProperty("--axis-height", `${42 + milestoneBandHeight}px`);

const renderAxis = () => {
  let months = "";
  let weeks = "";
  visibleDates.forEach((value, index) => {
    const date = parseDate(value);
    const left = LABEL_WIDTH + index * PX;
    const previous = index > 0 ? parseDate(visibleDates[index - 1]) : undefined;
    if (!previous || date.getMonth() !== previous.getMonth()) months += `<span class="axis-month" style="left:${left + 4}px">${date.getMonth() + 1}月</span>`;
    if (index === 0 || date.getDay() === 1) weeks += `<span class="axis-week" style="left:${left + 3}px">${date.getDate()}日</span>`;
  });
  const milestoneLabels = [...milestoneGroups.values()].map(group =>
    `<span class="axis-milestone-label" style="left:${group.left}px">${escapeHTML(group.labels.join("・"))}</span>`
  ).join("");
  return `<div class="axis"><div class="axis-label">タスク / 担当・計画</div>${months}${weeks}<div class="axis-milestones">${milestoneLabels}</div></div>`;
};

const renderBackground = () => {
  let bands = "";
  visibleDates.forEach((iso, index) => {
    const left = LABEL_WIDTH + index * PX;
    if (holidays.has(iso)) bands += `<div class="day-band holiday" style="left:${left}px;width:${PX}px"></div>`;
    if (halfDays.has(iso)) bands += `<div class="day-band half-day" style="left:${left}px;width:${PX}px"></div>`;
    if (bufferDates.has(iso)) bands += `<div class="day-band buffer" style="left:${left}px;width:${PX}px"></div>`;
  });
  let sprintBoundaries = "";
  if (data.SprintAnchor && data.SprintLengthDays > 0) {
    const boundary = parseDate(data.SprintAnchor);
    while (boundary > firstDate) boundary.setDate(boundary.getDate() - data.SprintLengthDays);
    for (; boundary <= lastDate; boundary.setDate(boundary.getDate() + data.SprintLengthDays)) {
      if (boundary >= firstDate) sprintBoundaries += `<div class="sprint-boundary" style="left:${boundaryPosition(isoDate(boundary))}px"></div>`;
    }
  }
  const lines = milestones.map(([date, , isToday]) =>
    `<div class="milestone${isToday ? " today" : ""}" style="left:${boundaryPosition(date)}px"></div>`
  ).join("");
  return `<div class="timeline-layer">${bands}${sprintBoundaries}${lines}</div>`;
};

const barClass = task => ["bar", task.Status === "done" ? "done" : "", task.Status === "in_progress" ? "in-progress" : "", task.Source === "fixed" ? "fixed" : ""].filter(Boolean).join(" ");
const taskRow = (task, showPlan, group) => {
  const left = taskStartPosition(task.Start, task.StartsInAfternoon);
  const end = taskEndPosition(task.End, task.EndsAtNoon);
  const width = Math.max(2, end - left);
  return `<div class="task-row"><div class="task-label" data-label-group="${group}"><span class="task-label-inner"><span class="task-id">${escapeHTML(task.ID)}</span>${showPlan ? `<span class="plan-chip">${escapeHTML(task.Plan)}</span>` : ""}<span class="task-plan">${escapeHTML(task.Name)}</span></span></div>
    <button type="button" class="${barClass(task)}" style="left:${left}px;width:${width}px;--worker-color:${workerColor[task.Assignee] || "var(--worker-default)"}" data-task-id="${escapeHTML(task.ID)}" aria-label="${escapeHTML(`${task.ID} ${task.Name} の詳細`)}"><span class="bar-label">${escapeHTML(task.Assignee)}</span></button></div>`;
};

const groupsFor = view => {
  if (view === "worker") {
    return data.Workers.map(worker => {
      const tasks = data.Tasks.filter(task => task.Assignee === worker).sort((left, right) => left.Start.localeCompare(right.Start) || left.ID.localeCompare(right.ID));
      const remaining = tasks.filter(task => task.Status !== "done").reduce((sum, task) => sum + task.Effort, 0);
      return { label: `${worker}（残 ${remaining.toFixed(1)}）`, tasks };
    }).filter(group => group.tasks.length);
  }
  return data.Plans.map(plan => ({ label: plan, tasks: data.Tasks.filter(task => task.Plan === plan).sort((left, right) => left.Start.localeCompare(right.Start) || left.ID.localeCompare(right.ID)) })).filter(group => group.tasks.length);
};

const renderGantt = view => {
  hideTooltip();
  const groups = groupsFor(view);
  const rows = groups.map((group, index) => `<div class="group-row"><span>${escapeHTML(group.label)}</span></div>${group.tasks.map(task => taskRow(task, view === "worker", index)).join("")}`).join("");
  gantt.innerHTML = `${renderAxis()}${renderBackground()}${rows || '<p class="empty-note">表示できるタスクがありません。</p>'}`;
  labelOffsets.clear();
};

const labelOffsets = new Map();
gantt.addEventListener("wheel", event => {
  const label = event.target.closest(".task-label");
  const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY)
    ? event.deltaX
    : event.shiftKey ? event.deltaY : 0;
  if (!label || !delta) return;
  const group = label.dataset.labelGroup;
  const labels = [...gantt.querySelectorAll(`.task-label[data-label-group="${group}"]`)];
  const maxOffset = Math.max(0, ...labels.map(candidate => {
    const inner = candidate.querySelector(".task-label-inner");
    return inner.scrollWidth - candidate.clientWidth;
  }));
  const current = labelOffsets.get(group) || 0;
  const next = Math.min(maxOffset, Math.max(0, current + delta));
  if (next === current) return;
  event.preventDefault();
  labelOffsets.set(group, next);
  for (const candidate of labels) {
    candidate.querySelector(".task-label-inner").style.transform = `translateX(${-next}px)`;
  }
}, { passive: false });

gantt.addEventListener("pointerover", event => {
  const bar = event.target.closest(".bar");
  if (!bar || (event.relatedTarget instanceof Node && bar.contains(event.relatedTarget))) return;
  showTooltip(bar, event);
});
gantt.addEventListener("pointerout", event => {
  const bar = event.target.closest(".bar");
  if (!bar || (event.relatedTarget instanceof Node && bar.contains(event.relatedTarget)) || bar === document.activeElement) return;
  hideTooltip(bar);
});
gantt.addEventListener("focusin", event => {
  const bar = event.target.closest(".bar");
  if (bar) showTooltip(bar);
});
gantt.addEventListener("focusout", event => {
  const bar = event.target.closest(".bar");
  if (bar) hideTooltip(bar);
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape") hideTooltip();
});
window.addEventListener("resize", () => hideTooltip());
gantt.closest(".gantt-scroll").addEventListener("scroll", () => hideTooltip());

const legend = document.getElementById("legend");
const workerLegend = data.Workers
  .filter(worker => data.Tasks.some(task => task.Assignee === worker))
  .map(worker => `<span class="legend-item"><span class="legend-mark worker" style="--worker-color:${workerColor[worker]}"></span>${escapeHTML(worker)}</span>`);
legend.innerHTML = [...workerLegend, ...[
  ["", "予定"], ["active", "着手中"], ["completed", "完了"], ["fixed", "固定期間"], ["holiday", "休日"], ["half-day", "半日稼働日"], ["buffer", "バッファ日"], ["sprint", "スプリント境界"]
].map(([style, text]) => `<span class="legend-item"><span class="legend-mark ${style}"></span>${escapeHTML(text)}</span>`)].join("");

document.getElementById("view-switch").addEventListener("click", event => {
  const button = event.target.closest("button[data-view]");
  if (!button) return;
  document.querySelectorAll("#view-switch button").forEach(candidate => candidate.setAttribute("aria-pressed", String(candidate === button)));
  renderGantt(button.dataset.view);
});
renderGantt("worker");
