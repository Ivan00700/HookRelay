"use strict";

const state = {
  endpoints: [],
  events: [],
  selectedEventID: "",
  endpointSnapshot: "",
  eventSnapshot: "",
  detailsSnapshot: "",
  sendingEvent: false,
};

const statusLabels = {
  pending: "Ожидает отправки или повтора",
  processing: "Обрабатывается",
  delivered: "Доставлено",
  failed: "Доставка завершилась ошибкой",
};

let refreshPromise = null;
let detailsRequest = 0;

function element(id) {
  return document.getElementById(id);
}

// все запросы идут в API того же сервера - секреты не сохраняются в браузерном хранилище
async function request(path, method = "GET", body) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 10000);
  const options = { method, signal: controller.signal, cache: "no-store" };
  if (body !== undefined) {
    options.headers = { "Content-Type": "application/json" };
    options.body = JSON.stringify(body);
  }
  try {
    const response = await fetch(path, options);
    const data = await response.json();
    if (!response.ok) {
      const message = data.error?.message || "Сервер не выполнил запрос";
      throw new Error(`HTTP ${response.status}: ${message}`);
    }
    return data;
  } catch (error) {
    if (error.name === "AbortError") {
      throw new Error("Сервер не ответил за 10 секунд. Обновите данные перед повторной отправкой.");
    }
    if (error instanceof TypeError) {
      throw new Error("Не удалось связаться с сервером. Проверьте соединение.");
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

function showMessage(id, text, kind = "success") {
  const output = element(id);
  output.textContent = text;
  output.className = `form-message ${kind}`;
}

function showError(id, text) {
  element(id).textContent = text;
  element(id).hidden = !text;
}

function formatTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("ru-RU");
}

// вставляем данные API как текст - имя и payload не должны становиться HTML
function cell(row, text, className = "") {
  const td = document.createElement("td");
  td.textContent = text;
  td.className = className;
  row.append(td);
  return td;
}

function emptyRow(target, columns, text) {
  const row = document.createElement("tr");
  const td = cell(row, text, "empty");
  td.colSpan = columns;
  target.replaceChildren(row);
}

function statusBadge(status) {
  const badge = document.createElement("span");
  badge.className = "badge";
  if (Object.prototype.hasOwnProperty.call(statusLabels, status)) {
    badge.classList.add(status);
    badge.title = statusLabels[status];
  }
  badge.textContent = status;
  return badge;
}

function endpointName(id) {
  const endpoint = state.endpoints.find(item => item.id === id);
  return endpoint ? endpoint.name : id;
}

function updateEventForm() {
  element("event-fields").disabled = state.endpoints.length === 0 || state.sendingEvent;
}

function renderEndpoints() {
  const rows = element("endpoint-rows");
  const select = element("event-endpoint");
  const previous = select.value;
  rows.replaceChildren();
  select.replaceChildren();
  if (state.endpoints.length === 0) {
    emptyRow(rows, 3, "Получателей пока нет. Добавьте первый endpoint выше.");
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "Сначала добавьте endpoint";
    select.append(option);
  }
  for (const endpoint of state.endpoints) {
    const row = document.createElement("tr");
    cell(row, endpoint.name);
    cell(row, endpoint.url, "mono small");
    cell(row, endpoint.id, "mono small");
    rows.append(row);
    const option = document.createElement("option");
    option.value = endpoint.id;
    option.textContent = endpoint.name;
    select.append(option);
  }
  if (state.endpoints.some(endpoint => endpoint.id === previous)) select.value = previous;
  updateEventForm();
}

function actionButton(text, action) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "button secondary compact";
  button.textContent = text;
  button.addEventListener("click", () => action(button));
  return button;
}

function renderEvents() {
  const rows = element("event-rows");
  rows.replaceChildren();
  if (state.events.length === 0) {
    emptyRow(rows, 5, "Событий пока нет. Отправьте первое событие через форму.");
    return;
  }
  const events = [...state.events].sort((a, b) => b.created_at.localeCompare(a.created_at) || a.id.localeCompare(b.id));
  for (const event of events) {
    const row = document.createElement("tr");
    if (event.id === state.selectedEventID) row.className = "selected";
    const time = cell(row, formatTime(event.created_at));
    const id = document.createElement("span");
    id.className = "cell-secondary mono";
    id.textContent = event.id;
    time.append(id);
    cell(row, event.type);
    cell(row, endpointName(event.endpoint_id));
    cell(row, "").append(statusBadge(event.status));
    const actions = document.createElement("div");
    actions.className = "actions";
    const details = actionButton("Попытки", () => openDetails(event.id));
    details.setAttribute("aria-label", `Попытки события ${event.id}`);
    actions.append(details);
    if (event.status === "failed") {
      const retry = actionButton("Повторить", button => retryEvent(event.id, button));
      retry.setAttribute("aria-label", `Повторить событие ${event.id}`);
      actions.append(retry);
    }
    cell(row, "").append(actions);
    rows.append(row);
  }
}

function renderDetails(event, attempts) {
  element("details-id").textContent = event.id;
  element("details-type").textContent = event.type;
  element("details-status").replaceChildren(statusBadge(event.status));
  element("details-payload").textContent = JSON.stringify(event.payload, null, 2);
  const rows = element("attempt-rows");
  rows.replaceChildren();
  if (attempts.length === 0) emptyRow(rows, 5, "Отправка ещё не началась.");
  for (const attempt of attempts) {
    const row = document.createElement("tr");
    cell(row, attempt.attempt_number);
    cell(row, formatTime(attempt.started_at));
    cell(row, attempt.finished_at ? formatTime(attempt.finished_at) : "В работе");
    cell(row, attempt.http_status ?? "—", "mono");
    cell(row, attempt.error_message || "—");
    rows.append(row);
  }
}

async function loadDetails() {
  const id = state.selectedEventID;
  if (!id) return;
  // номер запроса защищает выбранное событие от запоздавшего ответа
  const currentRequest = ++detailsRequest;
  try {
    const path = `/api/v1/events/${encodeURIComponent(id)}`;
    const [event, attempts] = await Promise.all([request(path), request(`${path}/attempts`)]);
    if (currentRequest !== detailsRequest || state.selectedEventID !== id) return;
    const snapshot = JSON.stringify([event, attempts]);
    if (snapshot !== state.detailsSnapshot) {
      renderDetails(event, attempts);
      state.detailsSnapshot = snapshot;
    }
    showError("details-error", "");
  } catch (error) {
    if (currentRequest === detailsRequest && state.selectedEventID === id) {
      showError("details-error", error.message);
    }
  }
}

function openDetails(id) {
  state.selectedEventID = id;
  state.detailsSnapshot = "";
  element("event-details").hidden = false;
  element("details-id").textContent = id;
  element("details-type").textContent = "—";
  element("details-status").textContent = "—";
  element("details-payload").textContent = "Загрузка…";
  emptyRow(element("attempt-rows"), 5, "Загрузка попыток…");
  showError("details-error", "");
  renderEvents();
  element("event-details").scrollIntoView({ block: "start" });
  return loadDetails();
}

// снимки позволяют не пересоздавать таблицы при каждом опросе без изменений
async function loadData() {
  element("refresh").disabled = true;
  try {
    const [endpoints, events] = await Promise.all([request("/api/v1/endpoints"), request("/api/v1/events")]);
    state.endpoints = endpoints;
    state.events = events;
    const endpointSnapshot = JSON.stringify(endpoints);
    const eventSnapshot = JSON.stringify(events);
    const endpointsChanged = endpointSnapshot !== state.endpointSnapshot;
    if (endpointsChanged) renderEndpoints();
    if (endpointsChanged || eventSnapshot !== state.eventSnapshot) renderEvents();
    state.endpointSnapshot = endpointSnapshot;
    state.eventSnapshot = eventSnapshot;
    element("endpoint-count").textContent = endpoints.length;
    element("event-count").textContent = events.length;
    element("delivered-count").textContent = events.filter(event => event.status === "delivered").length;
    element("failed-count").textContent = events.filter(event => event.status === "failed").length;
    element("last-updated").textContent = `Обновлено в ${new Date().toLocaleTimeString("ru-RU")}`;
    showError("page-error", "");
    await loadDetails();
  } catch (error) {
    showError("page-error", `Не удалось обновить данные: ${error.message}`);
  } finally {
    element("refresh").disabled = false;
  }
}

async function refreshData(afterChange = false) {
  // после записи ждём старый опрос и запрашиваем уже обновлённое состояние
  if (afterChange === true && refreshPromise) await refreshPromise;
  if (!refreshPromise) {
    refreshPromise = loadData().finally(() => { refreshPromise = null; });
  }
  return refreshPromise;
}

async function createEndpoint(event) {
  event.preventDefault();
  element("endpoint-fields").disabled = true;
  showMessage("endpoint-message", "Создание endpoint…");
  try {
    const endpoint = await request("/api/v1/endpoints", "POST", {
      name: element("endpoint-name").value,
      url: element("endpoint-url").value,
      secret: element("endpoint-secret").value,
    });
    // очищаем секрет после успешного создания endpoint
    element("endpoint-form").reset();
    showMessage("endpoint-message", "Endpoint создан. Теперь можно отправить событие.");
    await refreshData(true);
    element("event-endpoint").value = endpoint.id;
  } catch (error) {
    showMessage("endpoint-message", error.message, "error");
  } finally {
    element("endpoint-fields").disabled = false;
  }
}

async function createEvent(event) {
  event.preventDefault();
  showMessage("event-message", "");
  let payload;
  try {
    payload = JSON.parse(element("event-payload").value);
  } catch {
    showMessage("event-message", "Проверьте JSON в payload: не удалось разобрать текст.", "error");
    return;
  }
  if (payload === null || typeof payload !== "object" || Array.isArray(payload)) {
    showMessage("event-message", "Payload должен быть JSON-объектом.", "error");
    return;
  }
  const input = { endpoint_id: element("event-endpoint").value, type: element("event-type").value, payload };
  state.sendingEvent = true;
  updateEventForm();
  showMessage("event-message", "Сохранение события…");
  try {
    // подтверждение создания означает очередь - результат доставки узнаём из списка
    const result = await request("/api/v1/events", "POST", input);
    showMessage("event-message", `Событие ${result.id} принято. Статус доставки — в таблице ниже.`);
    await refreshData(true);
  } catch (error) {
    showMessage("event-message", error.message, "error");
  } finally {
    state.sendingEvent = false;
    updateEventForm();
  }
}

async function retryEvent(id, button) {
  // повтор начинает новый цикл - прежняя история остаётся на сервере
  button.disabled = true;
  showMessage("action-message", "Повторная постановка события в очередь…");
  try {
    await request(`/api/v1/events/${encodeURIComponent(id)}/retry`, "POST");
    showMessage("action-message", "Событие поставлено в очередь повторно. Прошлые попытки сохранены.");
    await refreshData(true);
  } catch (error) {
    showMessage("action-message", error.message, "error");
    await refreshData(true);
  } finally {
    button.disabled = false;
  }
}

element("endpoint-form").addEventListener("submit", createEndpoint);
element("event-form").addEventListener("submit", createEvent);
element("endpoint-fields").disabled = false;
element("refresh").addEventListener("click", () => refreshData());
element("close-details").addEventListener("click", () => {
  state.selectedEventID = "";
  detailsRequest++;
  element("event-details").hidden = true;
  renderEvents();
});
// скрытую вкладку не опрашиваем - ручное обновление остаётся доступным
setInterval(() => {
  if (element("auto-refresh").checked && !document.hidden) refreshData();
}, 3000);
refreshData();
