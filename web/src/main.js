import "./style.css";

const INSTALL = {
  brew: "brew install alderwork/tap/eme",
  curl: "curl -fsSL https://eme.jinmu.me/install.sh | sh",
  go: "go install github.com/alderwork/eme/cmd/eme@latest",
};

function initInstaller() {
  const code = document.getElementById("install-cmd");
  const copy = document.getElementById("copy");
  const tabs = document.querySelectorAll(".tab");
  if (!code || !copy) return;

  tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      tabs.forEach((t) => t.classList.remove("active"));
      tab.classList.add("active");
      code.textContent = INSTALL[tab.dataset.cmd];
    });
  });

  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(code.textContent.trim());
      const label = copy.querySelector(".copy-label");
      const original = label.textContent;
      const icon = copy.querySelector("svg").innerHTML;
      copy.classList.add("done");
      label.textContent = "Copied";
      copy.querySelector("svg").innerHTML = '<polyline points="20 6 9 17 4 12"></polyline>';
      setTimeout(() => {
        copy.classList.remove("done");
        label.textContent = original;
        copy.querySelector("svg").innerHTML = icon;
      }, 2000);
    } catch {
      /* clipboard unavailable — ignore */
    }
  });
}

async function initStars() {
  const el = document.getElementById("stars");
  if (!el) return;
  try {
    const res = await fetch("https://api.github.com/repos/alderwork/eme");
    if (!res.ok) return;
    const { stargazers_count } = await res.json();
    if (typeof stargazers_count === "number") {
      el.textContent = `GitHub ★ ${stargazers_count.toLocaleString()}`;
    }
  } catch {
    /* keep the plain "GitHub" label */
  }
}

function init() {
  initInstaller();
  initStars();
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
