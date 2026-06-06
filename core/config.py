"""Конфиг приложения. Загружается из config.json в директории скрипта."""
import json
from dataclasses import dataclass, asdict, field
from pathlib import Path
from typing import List, Optional


@dataclass
class DisplayConfig:
    port: str = "AUTO"
    baudrate: int = 230400   # 115200 надёжно; 230400 быстрее; 460800/921600 — шумят/виснут
    width: int = 480
    height: int = 320
    orientation: str = "landscape"
    brightness: int = 100
    protocol: str = "turing"
    framebuffer_addr: int = 0x001000


@dataclass
class UIConfig:
    theme: str = "gengar"
    preview_window: bool = True
    preview_scale: int = 2
    framerate: int = 5


@dataclass
class UserConfig:
    """Пользовательские настройки (редактируются через GUI)."""
    opencode_url: str = "https://opencode.ai/workspace/wrk_01KK7F1MHNJN72MSQH0A8YFXVF"
    opencode_cookie: str = ""   # значение cookie 'auth' (или оставить в файле)
    opencode_token: str = ""    # альтернатива — bearer token
    disks: List[str] = field(default_factory=lambda: ["C:\\", "Z:\\"])
    network_type: str = "wifi"  # "wifi" | "ethernet"
    ethernet_iface: str = ""    # имя интерфейса Ethernet (psutil)
    autostart: bool = False
    minimize_to_tray: bool = True


@dataclass
class Config:
    display: DisplayConfig = field(default_factory=DisplayConfig)
    ui: UIConfig = field(default_factory=UIConfig)
    user: UserConfig = field(default_factory=UserConfig)
    log_level: str = "INFO"

    @classmethod
    def load(cls, path: Path) -> "Config":
        if not path.exists():
            return cls()
        try:
            data = json.loads(path.read_text("utf-8"))
        except Exception:
            return cls()
        d = DisplayConfig(**{k: v for k, v in data.get("display", {}).items()
                             if k in DisplayConfig.__dataclass_fields__})
        u = UIConfig(**{k: v for k, v in data.get("ui", {}).items()
                        if k in UIConfig.__dataclass_fields__})
        usr = UserConfig(**{k: v for k, v in data.get("user", {}).items()
                            if k in UserConfig.__dataclass_fields__})
        return cls(display=d, ui=u, user=usr,
                   log_level=data.get("log_level", "INFO"))

    def save(self, path: Path) -> None:
        path.write_text(json.dumps({
            "display": asdict(self.display),
            "ui": asdict(self.ui),
            "user": asdict(self.user),
            "log_level": self.log_level,
        }, indent=2, ensure_ascii=False), "utf-8")


def default_config_path() -> Path:
    return Path(__file__).parent / "config.json"
