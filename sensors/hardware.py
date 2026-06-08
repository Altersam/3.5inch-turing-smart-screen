"""Сбор данных с железа. Используется с деградацией, если модуль недоступен."""
from __future__ import annotations
import time
import shutil
import socket
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional, List, Dict

try:
    import psutil
except Exception:
    psutil = None

try:
    import pynvml
    _NVML_OK = True
except Exception:
    _NVML_OK = False

try:
    import wmi
    _WMI_OK = True
except Exception:
    _WMI_OK = False


def _safe(fn, default):
    try:
        return fn()
    except Exception:
        return default


@dataclass
class NetSample:
    rx_bps: float = 0.0
    tx_bps: float = 0.0
    rx_total: int = 0
    tx_total: int = 0


@dataclass
class CpuSample:
    usage: float = 0.0
    freq_ghz: float = 0.0
    temp_c: Optional[float] = None
    name: str = "CPU"
    cores: int = 1
    per_core: List[float] = field(default_factory=list)


@dataclass
class GpuSample:
    usage: float = 0.0
    temp_c: Optional[float] = None
    vram_used_mb: float = 0.0
    vram_total_mb: float = 0.0
    name: str = "GPU"
    power_w: Optional[float] = None
    fan_pct: Optional[int] = None


@dataclass
class MemSample:
    used_gb: float = 0.0
    total_gb: float = 0.0
    percent: float = 0.0


@dataclass
class DiskSample:
    used_gb: float = 0.0
    total_gb: float = 0.0
    percent: float = 0.0
    temp_c: Optional[float] = None
    name: str = "Disk"
    mount: str = ""


@dataclass
class WifiSample:
    ssid: str = ""
    signal_pct: int = 0
    rx_bps: float = 0.0
    tx_bps: float = 0.0
    iface: str = ""


@dataclass
class OpenCodeSample:
    balance: str = "—.-- $"
    plan: str = ""
    raw_ok: bool = False
    error: str = ""


@dataclass
class Snapshot:
    cpu: CpuSample = field(default_factory=CpuSample)
    gpu: GpuSample = field(default_factory=GpuSample)
    mem: MemSample = field(default_factory=MemSample)
    disk: DiskSample = field(default_factory=DiskSample)
    disks: Dict[str, DiskSample] = field(default_factory=dict)
    net: NetSample = field(default_factory=NetSample)
    wifi: WifiSample = field(default_factory=WifiSample)
    opencode: OpenCodeSample = field(default_factory=OpenCodeSample)
    hostname: str = socket.gethostname()
    boot_time: float = 0.0


def list_disk_mounts() -> List[str]:
    """Возвращает список реально смонтированных дисков (Windows: C:\\, D:\\, ...)."""
    out = []
    if psutil:
        try:
            for p in psutil.disk_partitions(all=False):
                mp = p.mountpoint
                if mp and mp not in out:
                    out.append(mp)
        except Exception:
            pass
    if not out:
        out = ["C:\\"]
    return out


def get_wifi_ssid() -> str:
    """SSID текущей Wi-Fi сети через netsh (Windows)."""
    try:
        out = subprocess.check_output(
            ["netsh", "wlan", "show", "interfaces"],
            timeout=2, creationflags=0x08000000
        ).decode("utf-8", "ignore")
        for line in out.splitlines():
            line = line.strip()
            if line.startswith("SSID") and ":" in line and "BSSID" not in line:
                return line.split(":", 1)[1].strip()
    except Exception:
        pass
    return ""


def get_wifi_signal_pct() -> int:
    """Signal % текущего Wi-Fi (0..100) из netsh."""
    try:
        out = subprocess.check_output(
            ["netsh", "wlan", "show", "interfaces"],
            timeout=2, creationflags=0x08000000
        ).decode("utf-8", "ignore")
        for line in out.splitlines():
            line = line.strip()
            if line.startswith("Signal") and "%" in line:
                num = "".join(ch for ch in line.split(":", 1)[1] if ch.isdigit())
                if num:
                    return int(num)
    except Exception:
        pass
    return 0


class HardwareMonitor:
    def __init__(self, extra_disks: Optional[List[str]] = None,
                 opencode_url: str = "https://opencode.ai/workspace/wrk_01KK7F1MHNJN72MSQH0A8YFXVF",
                 opencode_cookie: str = "",
                 opencode_token: str = "",
                 network_type: str = "wifi",
                 ethernet_iface: str = "",
                 opencode_ttl: float = 60.0):
        self._wmi = None
        if _WMI_OK:
            try:
                self._wmi = wmi.WMI(namespace="root\\wmi")
            except Exception:
                self._wmi = None
        self._nvml_inited = False
        if _NVML_OK:
            try:
                pynvml.nvmlInit()
                self._nvml_inited = True
            except Exception:
                self._nvml_inited = False
        self._gpu_handle = None
        if self._nvml_inited:
            try:
                self._gpu_handle = pynvml.nvmlDeviceGetHandleByIndex(0)
            except Exception:
                self._gpu_handle = None
        self._last_net = psutil.net_io_counters() if psutil else None
        self._last_t = time.time()

        self.extra_disks = extra_disks or ["C:\\", "Z:\\"]
        self.opencode_url = opencode_url
        self.opencode_cookie = opencode_cookie
        self.opencode_token = opencode_token
        self.network_type = network_type
        self.ethernet_iface = ethernet_iface
        self.opencode_ttl = opencode_ttl
        self._last_oc = 0.0
        self._oc_cache: Optional["OpenCodeSample"] = None

    # === сеттеры для GUI ===
    def set_disks(self, disks: List[str]) -> None:
        """Список путей к дискам, которые нужно отображать (1 или 2)."""
        cleaned = []
        seen = set()
        for d in disks or []:
            if not d:
                continue
            norm = d.rstrip("/\\") + "\\"
            if norm in seen:
                continue
            seen.add(norm)
            cleaned.append(d)
        self.extra_disks = cleaned

    def set_network_type(self, kind: str, iface: str = "") -> None:
        """Переключение WiFi/Ethernet. iface — имя интерфейса Ethernet (psutil)."""
        self.network_type = "ethernet" if kind == "ethernet" else "wifi"
        if self.network_type == "ethernet" and iface:
            self.ethernet_iface = iface

    def set_opencode(self, url: str = "", cookie: str = "", token: str = "") -> None:
        if url:
            self.opencode_url = url
        if cookie is not None:
            self.opencode_cookie = cookie
        if token is not None:
            self.opencode_token = token
        self._oc_cache = None
        self._last_oc = 0.0

    def _cpu_temp_wmi(self):
        if self._wmi:
            try:
                ts = self._wmi.MSAcpi_ThermalZoneTemperature()
                if ts:
                    temps = []
                    for t in ts:
                        c = (t.CurrentTemperature / 10.0) - 273.15
                        if 0 < c < 120:
                            temps.append(c)
                    if temps:
                        return max(temps)
            except Exception:
                pass
        # fallback: OpenHardwareMonitor WMI
        try:
            import wmi as _wmi_mod
            ohm = _wmi_mod.WMI(namespace="root\\OpenHardwareMonitor\\Hardware")
            sensors = ohm.Sensor()
            cpu_temps = []
            for s in sensors:
                if s.SensorType == "Temperature" and "CPU" in s.Parent:
                    try:
                        v = float(s.Value)
                        if 0 < v < 120:
                            cpu_temps.append(v)
                    except Exception:
                        pass
            if cpu_temps:
                return max(cpu_temps)
        except Exception:
            pass
        # fallback: typeperf one-shot
        try:
            out = subprocess.check_output(
                ["typeperf", "\\Thermal Zone Information(_Total)\\Temperature",
                 "-sc", "1"], timeout=3,
                creationflags=0x08000000
            ).decode("utf-8", "ignore")
            for line in out.splitlines():
                if line.startswith('"') and "," in line:
                    val = line.split(",")[1].strip().strip('"')
                    c = (float(val) / 10.0) - 273.15
                    if 0 < c < 120:
                        return c
        except Exception:
            pass
        return None

    def _disk_temp_wmi(self):
        if not self._wmi:
            return None
        try:
            ts = self._wmi.MSAcpi_ThermalZoneTemperature()
            if len(ts) > 1:
                return (ts[1].CurrentTemperature / 10.0) - 273.15
        except Exception:
            return None

    def _cpu_freq_ghz(self) -> float:
        if not psutil:
            return 0.0
        try:
            f = psutil.cpu_freq()
            if f and f.current:
                return f.current / 1000.0
        except Exception:
            pass
        return 0.0

    def _disk_sample(self, mount: str) -> DiskSample:
        ds = DiskSample(mount=mount, name=mount.rstrip(":\\").rstrip("/") or "Disk")
        if not psutil:
            return ds
        try:
            d = psutil.disk_usage(mount)
            ds.used_gb = d.used / (1024 ** 3)
            ds.total_gb = d.total / (1024 ** 3)
            ds.percent = d.percent
        except Exception:
            pass
        return ds

    def _wifi_sample(self) -> WifiSample:
        """Возвращает данные сети. В режиме wifi — SSID и сигнал,
        в режиме ethernet — имя интерфейса (если задано)."""
        w = WifiSample()
        if self.network_type == "ethernet":
            w.ssid = "Ethernet"
            w.signal_pct = 100   # кабель = 100%
            if self.ethernet_iface:
                w.iface = self.ethernet_iface
            elif psutil:
                try:
                    stats = psutil.net_if_stats()
                    addrs = psutil.net_if_addrs()
                    eth = [n for n in stats
                           if "ethernet" in n.lower() or "eth" in n.lower().split()]
                    eth = [n for n in eth if stats[n].isup and n in addrs]
                    if eth:
                        w.iface = eth[0]
                except Exception:
                    pass
            return w

        # Wi-Fi
        w.ssid = get_wifi_ssid()
        w.signal_pct = get_wifi_signal_pct()
        if psutil:
            try:
                stats = psutil.net_if_stats()
                addrs = psutil.net_if_addrs()
                wifi_names = [n for n in stats
                              if "wi-fi" in n.lower() or "wifi" in n.lower() or
                              "wlan" in n.lower() or "wireless" in n.lower()]
                if not wifi_names:
                    wifi_names = [n for n in stats
                                  if stats[n].isup and n in addrs]
                if wifi_names:
                    w.iface = wifi_names[0]
            except Exception:
                pass
        return w

    def _opencode_sample(self) -> OpenCodeSample:
        """Запрос к OpenCode workspace (кеш N сек).
        Приоритет: opencode_token (Authorization header) → opencode_cookie → файл.
        """
        now = time.time()
        if self._oc_cache and (now - self._last_oc) < self.opencode_ttl:
            return self._oc_cache

        o = OpenCodeSample()

        token = (self.opencode_token or "").strip()
        cookie = (self.opencode_cookie or "").strip()
        if not token and not cookie:
            cookie_path = Path(__file__).parent.parent / "opencode_cookie.txt"
            if cookie_path.exists():
                try:
                    cookie = cookie_path.read_text("utf-8").strip()
                except Exception:
                    cookie = ""
            token_path = Path(__file__).parent.parent / "opencode_token.txt"
            if not token and token_path.exists():
                try:
                    token = token_path.read_text("utf-8").strip()
                except Exception:
                    token = ""

        if not token and not cookie:
            o.balance = "—.— $"
            o.plan = "no-auth"
            o.error = "set cookie/token in GUI"
            self._oc_cache = o; self._last_oc = now
            return o

        try:
            import urllib.request, re
            headers = {"User-Agent": "Mozilla/5.0"}
            if token:
                headers["Authorization"] = f"Bearer {token}"
            if cookie:
                headers["Cookie"] = f"oc_locale=ru; auth={cookie}"
            req = urllib.request.Request(self.opencode_url, headers=headers)
            with urllib.request.urlopen(req, timeout=5) as r:
                body = r.read().decode("utf-8", "ignore")

            balance_m = re.search(r'balance:(\d+)', body)
            monthly_limit_m = re.search(r'monthlyLimit:(\d+)', body)
            monthly_usage_m = re.search(r'monthlyUsage:(\d+)', body)
            sub_m = re.search(r'subscription:(\w+|"[^"]*")', body)
            lite_m = re.search(r'liteSubscriptionID:"([^"]+)"', body)

            if not balance_m:
                o.balance = "—.— $"
                o.plan = "no-data"
                o.error = "balance not in response"
                self._oc_cache = o; self._last_oc = now
                return o

            balance_raw = int(balance_m.group(1))
            monthly_limit = int(monthly_limit_m.group(1)) if monthly_limit_m else 0
            monthly_usage = int(monthly_usage_m.group(1)) if monthly_usage_m else 0

            balance_usd = balance_raw / 100_000_000.0
            o.balance = f"{balance_usd:,.2f} $"
            o.raw_ok = True

            sub_val = ""
            if sub_m:
                v = sub_m.group(1)
                if v.startswith('"') and v.endswith('"'):
                    sub_val = v[1:-1]
                else:
                    sub_val = v
            if sub_val and sub_val != "null":
                o.plan = sub_val
            elif lite_m:
                o.plan = "lite"
            else:
                o.plan = "free"

            if monthly_usage and monthly_limit:
                o.error = f"used {monthly_usage/100_000_000:.2f}/{monthly_limit} $"

        except Exception as e:
            o.balance = "—.— $"
            o.plan = "err"
            o.error = str(e)[:60]

        self._oc_cache = o
        self._last_oc = now
        return o

    def snapshot(self) -> Snapshot:
        snap = Snapshot()
        snap.boot_time = psutil.boot_time() if psutil else 0

        # CPU
        if psutil:
            snap.cpu.per_core = psutil.cpu_percent(interval=None, percpu=True) or [0.0]
            snap.cpu.usage = (sum(snap.cpu.per_core) / len(snap.cpu.per_core)
                              if snap.cpu.per_core else 0.0)
            snap.cpu.cores = psutil.cpu_count(logical=True) or len(snap.cpu.per_core)
            snap.cpu.freq_ghz = self._cpu_freq_ghz()
            snap.cpu.temp_c = self._cpu_temp_wmi()
        snap.cpu.name = self._cpu_name()

        # MEM
        if psutil:
            vm = psutil.virtual_memory()
            snap.mem.used_gb = vm.used / (1024 ** 3)
            snap.mem.total_gb = vm.total / (1024 ** 3)
            snap.mem.percent = vm.percent

        # DISKS
        main_mount = "C:\\" if psutil and hasattr(psutil, "WINDOWS") and psutil.WINDOWS else "/"
        snap.disk = self._disk_sample(main_mount)
        snap.disk.temp_c = self._disk_temp_wmi()
        for mount in self.extra_disks:
            if not mount:
                continue
            m_norm = mount.rstrip("/\\") + "\\"
            if m_norm == snap.disk.mount:
                continue
            snap.disks[mount] = self._disk_sample(mount)

        # NET
        now = time.time()
        if psutil and self._last_net is not None:
            cur = psutil.net_io_counters()
            dt = max(0.001, now - self._last_t)
            snap.net.rx_bps = (cur.bytes_recv - self._last_net.bytes_recv) / dt
            snap.net.tx_bps = (cur.bytes_sent - self._last_net.bytes_sent) / dt
            snap.net.rx_total = cur.bytes_recv
            snap.net.tx_total = cur.bytes_sent
            self._last_net = cur
            self._last_t = now

        # WiFi
        snap.wifi = self._wifi_sample()
        snap.wifi.rx_bps = snap.net.rx_bps
        snap.wifi.tx_bps = snap.net.tx_bps

        # OpenCode
        snap.opencode = self._opencode_sample()

        # GPU
        if self._nvml_inited and self._gpu_handle:
            try:
                util = pynvml.nvmlDeviceGetUtilizationRates(self._gpu_handle)
                snap.gpu.usage = util.gpu
                try:
                    snap.gpu.temp_c = pynvml.nvmlDeviceGetTemperature(
                        self._gpu_handle, pynvml.NVML_TEMPERATURE_GPU)
                except Exception:
                    pass
                mem = pynvml.nvmlDeviceGetMemoryInfo(self._gpu_handle)
                snap.gpu.vram_used_mb = mem.used / (1024 ** 2)
                snap.gpu.vram_total_mb = mem.total / (1024 ** 2)
                try:
                    snap.gpu.name = pynvml.nvmlDeviceGetName(
                        self._gpu_handle).decode("utf-8", "ignore")
                except Exception:
                    pass
                try:
                    p = pynvml.nvmlDeviceGetPowerUsage(self._gpu_handle)
                    snap.gpu.power_w = p / 1000.0
                except Exception:
                    pass
            except Exception:
                pass

        return snap

    @staticmethod
    def _cpu_name() -> str:
        try:
            out = subprocess.check_output(
                ["wmic", "cpu", "get", "name"], timeout=2,
                creationflags=0x08000000).decode("utf-8", "ignore")
            for line in out.splitlines():
                line = line.strip()
                if line and line.lower() != "name":
                    return line
        except Exception:
            pass
        return "CPU"
