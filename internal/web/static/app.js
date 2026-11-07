"use strict";

// Поиск работает обычной HTML-формой. JavaScript добавляет только фоновое обновление.
const refreshButton = document.querySelector("[data-refresh]");
const refreshStatus = document.querySelector("[data-refresh-status]");

async function requestJSON(url, options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(url, {
      ...options,
      signal: controller.signal,
      headers: { Accept: "application/json", ...options.headers },
      credentials: "same-origin",
      cache: "no-store",
    });
    if (!response.ok) {
      if (response.status === 429) {
        throw new Error("Слишком много запросов. Подождите немного и повторите обновление.");
      }
      if (response.status === 503) {
        throw new Error("Сервис временно недоступен. Попробуйте обновить данные позже.");
      }
      throw new Error("Не удалось обновить данные. Сервер вернул ошибку " + response.status + ".");
    }
    return await response.json();
  } finally {
    clearTimeout(timer);
  }
}

function setRefreshMessage(message, isError = false) {
  if (!refreshStatus) return;
  refreshStatus.textContent = message;
  refreshStatus.classList.toggle("is-error", isError);
}

if (refreshButton && refreshStatus) {
  refreshButton.hidden = false;
  refreshButton.addEventListener("click", async () => {
    refreshButton.disabled = true;
    refreshButton.setAttribute("aria-busy", "true");
    setRefreshMessage("Загружаем четыре раздела API. Каталог остаётся доступным…");
    try {
      await requestJSON("/api/refresh", { method: "POST" });
      const deadline = Date.now() + 60000;
      // Сервер выполняет работу отдельно; здесь только проверяется её результат.
      while (Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 900));
        const status = await requestJSON("/api/status");
        if (!status || typeof status.refreshing !== "boolean") {
          throw new Error("Сервер вернул неожиданный ответ. Попробуйте проверить данные позже.");
        }
        if (!status.refreshing) {
          if (status.last_error) {
            throw new Error("Источник не ответил корректно. Сохранённые данные остаются доступны. Попробуйте позже.");
          }
          setRefreshMessage("Данные обновлены. Открываем свежий каталог…");
          window.location.reload();
          return;
        }
      }
      throw new Error("Обновление занимает больше минуты. Попробуйте проверить результат позже.");
    } catch (error) {
      let message = "Нет связи с сервером. Проверьте подключение и попробуйте позже.";
      if (error instanceof Error && error.name === "AbortError") {
        message = "Сервер не ответил вовремя. Каталог можно продолжать просматривать.";
      } else if (error instanceof Error && !(error instanceof TypeError) && !(error instanceof SyntaxError)) {
        message = error.message;
      }
      setRefreshMessage(message, true);
    } finally {
      refreshButton.disabled = false;
      refreshButton.removeAttribute("aria-busy");
    }
  });
}

// Ошибка внешнего изображения не скрывает имя и данные артиста.
for (const picture of document.querySelectorAll(".artist-image")) {
  const showFallback = () => {
    if (picture.hidden) return;
    picture.hidden = true;
    const caption = document.createElement("span");
    caption.className = "image-fallback";
    caption.textContent = "Изображение недоступно";
    picture.parentElement.append(caption);
  };
  picture.addEventListener("error", showFallback, { once: true });
  if (picture.complete && picture.naturalWidth === 0) showFallback();
}
