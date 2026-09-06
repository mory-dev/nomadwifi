using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Forms;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;
using NomadWiFi.UI.Models;
using NomadWiFi.UI.Services;
using Application = System.Windows.Application;
using Button = System.Windows.Controls.Button;
using KeyEventArgs = System.Windows.Input.KeyEventArgs;
using Point = System.Windows.Point;

namespace NomadWiFi.UI
{
    public partial class MainWindow : Window
    {
        private readonly AgentClient _agent = new AgentClient();
        private readonly DispatcherTimer _pollTimer = new DispatcherTimer();
        private readonly List<double> _latencyHistory = new List<double>();

        private NotifyIcon _notifyIcon;
        private ToolStripMenuItem _trayMenuAutoRoam;
        private ToolStripMenuItem _trayMenuStartup;

        private bool _isBusy;
        private bool _isExiting;
        private bool _isAutoRoamEnabled = true;
        private TrayState _trayState = TrayState.Offline;
        private string _captivePortalUrl = "http://neverssl.com";
        private string _pendingModalSsid = "";
        private List<AccessPoint> _currentAps = new List<AccessPoint>();
        private VpnStatus _vpnStatus;
        private bool _scanRefreshPending;

        public MainWindow() : this(false) { }

        public MainWindow(bool startMinimized)
        {
            InitializeComponent();

            Icon = Branding.WindowIcon();
            ImgBrandMark.Source = Branding.WindowIcon();
            SetupSystemTray();

            ChkAutoRoam.IsChecked = _isAutoRoamEnabled;
            // Correct a stale entry before reading it: the app may have been
            // moved since it was registered.
            StartupManager.RefreshStartupPath();
            ChkStartup.IsChecked = StartupManager.IsStartupEnabled();

            _agent.EventReceived += OnAgentEvent;
            _agent.Disconnected += OnAgentDisconnected;
            _agent.Start();

            // The window only refreshes while it is on screen. Every roaming
            // decision lives in the core process, so nothing depends on the
            // window being open.
            _pollTimer.Interval = TimeSpan.FromSeconds(5);
            _pollTimer.Tick += async (s, e) =>
            {
                if (!IsVisible || WindowState == WindowState.Minimized || !IsActive) return;
                await RefreshAllAsync();
            };
            _pollTimer.Start();

            Loaded += async (s, e) =>
            {
                FitToWorkArea();
                // Before the minimised early-return: a start-with-Windows
                // launch still needs the tray menu's checkmark to match.
                await SyncAutoRoamAsync();
                if (startMinimized) { Hide(); return; }
                await RefreshAllAsync();
                await CheckForUpdateAsync();
            };

            Activated += async (s, e) =>
            {
                if (IsVisible && WindowState != WindowState.Minimized) await RefreshAllAsync();
            };

            StateChanged += async (s, e) =>
            {
                if (WindowState != WindowState.Minimized && IsVisible) await RefreshAllAsync();
            };

            Closing += MainWindow_Closing;
        }

        /// <summary>
        /// Shrinks and re-centres the window if it does not fit the screen.
        ///
        /// The default size is comfortable on a desktop, but a small laptop at
        /// 150% scaling has well under 900 logical pixels of height, and
        /// without this the title bar opens above the top of the screen.
        /// </summary>
        private void FitToWorkArea()
        {
            var area = SystemParameters.WorkArea;
            if (area.Width <= 0 || area.Height <= 0) return;

            const double margin = 24;
            var maxWidth = Math.Max(MinWidth, area.Width - margin);
            var maxHeight = Math.Max(MinHeight, area.Height - margin);

            if (Width > maxWidth) Width = maxWidth;
            if (Height > maxHeight) Height = maxHeight;

            Left = area.Left + Math.Max(0, (area.Width - Width) / 2);
            Top = area.Top + Math.Max(0, (area.Height - Height) / 2);
        }

        #region Core agent events

        private void OnAgentEvent(string name, object payload)
        {
            Dispatcher.BeginInvoke(new Action(async () =>
            {
                if (name == "log")
                {
                    var message = ReadStringField(payload, "message");
                    if (!string.IsNullOrEmpty(message)) TxtActivity.Text = message;
                    return;
                }

                if (name == "roamed")
                {
                    var to = ReadStringField(payload, "To");
                    var label = string.IsNullOrEmpty(to) ? "a better access point" : to;
                    TxtRoamCelebrationMsg.Text = "Moved to " + label + " for a faster, steadier connection.";
                    BorderRoamCelebration.Visibility = Visibility.Visible;
                    Notify("Switched access point", "Now connected to " + label + ".");
                    await RefreshAllAsync();
                }
            }));
        }

        private void OnAgentDisconnected()
        {
            Dispatcher.BeginInvoke(new Action(() =>
            {
                if (_agent.GaveUp)
                {
                    // Restarting an executable that refuses to run just spawns
                    // processes in a loop; say what is wrong instead.
                    TxtActivity.Text = "The NomadWiFi engine could not be started.";
                    ShowEngineUnavailable();
                    return;
                }

                TxtActivity.Text = "The NomadWiFi core stopped; restarting it.";
                _agent.Start();
            }));
        }

        private static string ReadStringField(object payload, string field)
        {
            var map = payload as Dictionary<string, object>;
            if (map == null) return null;
            object value;
            if (!map.TryGetValue(field, out value) || value == null) return null;
            return value.ToString();
        }

        private static object ReadField(object payload, string field)
        {
            var map = payload as Dictionary<string, object>;
            if (map == null) return null;
            object value;
            return map.TryGetValue(field, out value) ? value : null;
        }

        private static bool ReadBoolField(object payload, string field)
        {
            var value = ReadField(payload, field);
            if (value is bool) return (bool)value;
            bool parsed;
            return value != null && bool.TryParse(value.ToString(), out parsed) && parsed;
        }

        #endregion

        #region System tray

        private void SetupSystemTray()
        {
            _notifyIcon = new NotifyIcon
            {
                Text = "NomadWiFi",
                Icon = Branding.TrayIcon(TrayState.Offline),
                Visible = true,
            };

            var menu = new ContextMenuStrip();
            menu.Items.Add("Open NomadWiFi", null, (s, e) => RestoreFromTray());
            menu.Items.Add("Switch to the best access point", null, async (s, e) => await OptimizeAsync());

            _trayMenuAutoRoam = new ToolStripMenuItem("Roam automatically", null,
                new EventHandler(async (s, e) => await SetAutoRoamAsync(!_isAutoRoamEnabled)))
            { Checked = _isAutoRoamEnabled };
            menu.Items.Add(_trayMenuAutoRoam);

            _trayMenuStartup = new ToolStripMenuItem("Start with Windows", null, new EventHandler((s, e) =>
            {
                var enable = !StartupManager.IsStartupEnabled();
                StartupManager.SetStartup(enable);
                _trayMenuStartup.Checked = enable;
                if (ChkStartup != null) ChkStartup.IsChecked = enable;
            }))
            { Checked = StartupManager.IsStartupEnabled() };
            menu.Items.Add(_trayMenuStartup);

            menu.Items.Add(new ToolStripSeparator());
            menu.Items.Add("Quit", null, (s, e) => ExitApplication());

            _notifyIcon.ContextMenuStrip = menu;
            _notifyIcon.DoubleClick += (s, e) => RestoreFromTray();
        }

        /// <summary>Shows connection quality in the tray without opening the window.</summary>
        private void SetTrayState(TrayState state)
        {
            if (_notifyIcon == null || state == _trayState) return;
            _trayState = state;
            _notifyIcon.Icon = Branding.TrayIcon(state);
        }

        private void Notify(string title, string message)
        {
            if (_notifyIcon != null) _notifyIcon.ShowBalloonTip(4000, title, message, ToolTipIcon.Info);
        }

        /// <summary>
        /// Brings the window back from the tray. Public so a second launch can
        /// surface the instance that is already running instead of starting
        /// another one.
        /// </summary>
        public void PresentToUser()
        {
            RestoreFromTray();
        }

        private async void RestoreFromTray()
        {
            Show();
            WindowState = WindowState.Normal;
            Activate();
            await RefreshAllAsync();
        }

        private void MainWindow_Closing(object sender, System.ComponentModel.CancelEventArgs e)
        {
            if (_isExiting) return;
            e.Cancel = true;
            Hide();
            Notify("NomadWiFi is still running", "It keeps watching your connection from the system tray.");
        }

        private void ExitApplication()
        {
            _isExiting = true;
            if (_notifyIcon != null)
            {
                _notifyIcon.Visible = false;
                _notifyIcon.Dispose();
            }
            _agent.Dispose();
            Application.Current.Shutdown();
        }

        #endregion

        #region Title bar

        private void TitleBar_MouseDown(object sender, MouseButtonEventArgs e)
        {
            if (e.ChangedButton != MouseButton.Left) return;
            if (e.ClickCount == 2) ToggleMaximize();
            else DragMove();
        }

        private void BtnMinimize_Click(object sender, RoutedEventArgs e)
        {
            WindowState = WindowState.Minimized;
        }

        private void BtnMaximize_Click(object sender, RoutedEventArgs e)
        {
            ToggleMaximize();
        }

        private void ToggleMaximize()
        {
            if (WindowState == WindowState.Maximized)
            {
                WindowState = WindowState.Normal;
                BtnMaximize.Content = "□";
            }
            else
            {
                WindowState = WindowState.Maximized;
                BtnMaximize.Content = "❐";
            }
        }

        private void BtnClose_Click(object sender, RoutedEventArgs e)
        {
            Hide();
            Notify("NomadWiFi is still running", "It keeps watching your connection from the system tray.");
        }

        #endregion

        #region Refresh

        private async Task RefreshAllAsync()
        {
            await RefreshStatusAsync();
            await RefreshScanAsync();
            await RefreshVpnAsync();
        }

        /// <summary>
        /// Shown when the background engine is not answering. It names the
        /// executable it tried so a wrongly-placed copy is obvious, rather
        /// than looking like an ordinary loss of Wi-Fi.
        /// </summary>
        private void ShowEngineUnavailable()
        {
            BorderOfflineBanner.Visibility = Visibility.Visible;
            BorderCaptivePortal.Visibility = Visibility.Collapsed;
            TxtOfflineBanner.Text =
                "The NomadWiFi engine is not running, so nothing below is live. "
                + "Run the app from a folder that has core\\nomadwifi.exe beside it.";

            TxtSsid.Text = "Engine not running";
            TxtDetails.Text = string.IsNullOrEmpty(_agent.LastError)
                ? "Tried: " + _agent.CorePath
                : _agent.CorePath + " said: " + _agent.LastError;
            TxtBand.Text = "No engine";
            TxtBand.Foreground = (Brush)FindResource("AccentRed");
            BorderBand.Background = Hex("#22F85149");
            BorderBand.BorderBrush = Hex("#44F85149");

            TxtSignal.Text = "--";
            TxtSignalSub.Text = "";
            TxtSpeed.Text = "--";
            TxtLatency.Text = "--";
            TxtJitter.Text = "";
            SetTrayState(TrayState.Offline);
        }

        private async Task RefreshStatusAsync()
        {
            var status = await _agent.CallAsync<InterfaceStatus>("status");

            // A silent core is not the same thing as a missing network. Saying
            // "choose a network below" when the engine never answered sends the
            // user to a list that cannot possibly work.
            if (status == null)
            {
                ShowEngineUnavailable();
                return;
            }

            if (!status.connected)
            {
                BorderOfflineBanner.Visibility = Visibility.Visible;
                TxtOfflineBanner.Text = "Not connected. Choose a network below to get online.";
                BorderCaptivePortal.Visibility = Visibility.Collapsed;

                TxtSsid.Text = "Not connected";
                TxtDetails.Text = "Choose a network below to get online.";
                TxtBand.Text = "Offline";
                TxtBand.Foreground = (Brush)FindResource("AccentRed");
                BorderBand.Background = Hex("#22F85149");
                BorderBand.BorderBrush = Hex("#44F85149");

                TxtSignal.Text = "--";
                TxtSignalSub.Text = "Signal";
                TxtSpeed.Text = "--";
                TxtLatency.Text = "--";
                TxtJitter.Text = "No connection";
                SetTrayState(TrayState.Offline);
                return;
            }

            BorderOfflineBanner.Visibility = Visibility.Collapsed;

            TxtSsid.Text = status.ssid;
            TxtDetails.Text = string.Format("{0}  ·  channel {1}  ·  {2}",
                string.IsNullOrEmpty(status.radio_type) ? "Wi-Fi" : status.radio_type,
                status.channel, status.bssid);

            TxtBand.Text = status.band;
            TxtBand.Foreground = (Brush)FindResource(status.IsFastBand ? "AccentBlue" : "AccentYellow");
            BorderBand.Background = Hex(status.IsFastBand ? "#2258A6FF" : "#22D29922");
            BorderBand.BorderBrush = Hex(status.IsFastBand ? "#4458A6FF" : "#44D29922");

            TxtSignal.Text = status.signal_percent + "%";
            TxtSignalSub.Text = status.rssi != 0 ? status.rssi + " dBm" : "Signal";
            TxtSpeed.Text = status.rx_mbps + " Mbps";
            TxtLatency.Text = string.Format("{0:F0} ms", status.gateway_latency_ms);

            if (status.captive_portal)
            {
                _captivePortalUrl = string.IsNullOrEmpty(status.captive_portal_url)
                    ? "http://neverssl.com" : status.captive_portal_url;
                BorderCaptivePortal.Visibility = Visibility.Visible;
                UpdateCaptivePortalAdvice();
            }
            else
            {
                BorderCaptivePortal.Visibility = Visibility.Collapsed;
            }

            SetTrayState(TrayStateFor(status));

            if (status.gateway_latency_ms > 0)
            {
                _latencyHistory.Add(status.gateway_latency_ms);
                if (_latencyHistory.Count > 30) _latencyHistory.RemoveAt(0);
                UpdateSparkline();
            }
        }

        private TrayState TrayStateFor(InterfaceStatus status)
        {
            if (_vpnStatus != null && _vpnStatus.active != null && status.captive_portal)
            {
                return TrayState.VpnHeld;
            }
            if (!status.IsFastBand || status.signal_percent < 40 || status.gateway_latency_ms > 150)
            {
                return TrayState.Slow;
            }
            return TrayState.Good;
        }

        private async Task RefreshScanAsync()
        {
            if (_isBusy)
            {
                // A password-success refresh must not disappear behind a
                // background poll that started at the same time.
                _scanRefreshPending = true;
                return;
            }

            _isBusy = true;
            try
            {
                do
                {
                    _scanRefreshPending = false;
                    var aps = await _agent.CallAsync<List<AccessPoint>>("scan");
                    if (aps != null)
                    {
                        _currentAps = aps;
                        ItemsAccessPoints.ItemsSource = aps;
                        TxtApCount.Text = aps.Count == 1 ? "1 network in range"
                                                         : aps.Count + " networks in range";
                    }
                }
                while (_scanRefreshPending);
            }
            finally
            {
                _isBusy = false;
            }
        }

        private async Task RefreshVpnAsync()
        {
            _vpnStatus = await _agent.CallAsync<VpnStatus>("vpn_status");

            if (_vpnStatus == null || _vpnStatus.tunnels == null || _vpnStatus.tunnels.Count == 0)
            {
                BorderVpn.Visibility = Visibility.Collapsed;
                return;
            }

            BorderVpn.Visibility = Visibility.Visible;
            var active = _vpnStatus.active;

            if (active != null)
            {
                TxtVpnProvider.Text = active.provider;
                TxtVpnState.Text = "Carrying all traffic";
                TxtVpnState.Foreground = (Brush)FindResource("AccentGreen");
                TxtVpnHint.Text = active.controllable
                    ? "NomadWiFi pauses and restores this automatically when it switches networks."
                    : active.control_hint;
                BtnVpnPause.Visibility = active.controllable ? Visibility.Visible : Visibility.Collapsed;
            }
            else
            {
                var first = _vpnStatus.tunnels.FirstOrDefault(t => t.up);
                if (first == null) first = _vpnStatus.tunnels[0];

                TxtVpnProvider.Text = first.provider;
                TxtVpnState.Text = first.up ? "Connected, not the default route" : "Installed, not connected";
                TxtVpnState.Foreground = (Brush)FindResource("TextSecondary");
                TxtVpnHint.Text = "No tunnel is carrying traffic right now.";
                BtnVpnPause.Visibility = Visibility.Collapsed;
            }

            UpdateCaptivePortalAdvice();
        }

        /// <summary>
        /// A kill switch stops the hotel login page from loading, which is the
        /// most confusing failure in a hotel. Say so plainly rather than
        /// letting the browser time out on its own.
        /// </summary>
        private void UpdateCaptivePortalAdvice()
        {
            if (BorderCaptivePortal.Visibility != Visibility.Visible) return;

            var blocked = _vpnStatus != null && _vpnStatus.active != null;
            TxtCaptiveMsg.Text = blocked
                ? "This network needs a web login, but " + _vpnStatus.active.provider +
                  " is carrying all traffic so the login page cannot load."
                : "This network needs a web login before you can get online.";
            BtnPauseVpnAndLogin.Visibility = blocked ? Visibility.Visible : Visibility.Collapsed;
        }

        private void UpdateSparkline()
        {
            if (_latencyHistory.Count < 2) return;

            var w = CanvasSparkline.ActualWidth > 10 ? CanvasSparkline.ActualWidth : 140;
            var h = CanvasSparkline.ActualHeight > 10 ? CanvasSparkline.ActualHeight : 34;

            var min = _latencyHistory.Min();
            var max = _latencyHistory.Max();
            if (Math.Abs(max - min) < 1.0) max = min + 5.0;

            var avg = _latencyHistory.Average();
            var jitter = _latencyHistory.Average(v => Math.Abs(v - avg));
            TxtJitter.Text = string.Format("Jitter {0:F0} ms", jitter);

            var line = new PointCollection();
            var area = new PointCollection();
            area.Add(new Point(0, h));

            var step = w / (_latencyHistory.Count - 1);
            for (int i = 0; i < _latencyHistory.Count; i++)
            {
                var y = h - ((_latencyHistory[i] - min) / (max - min) * (h - 6)) - 3;
                var point = new Point(i * step, y);
                line.Add(point);
                area.Add(point);
            }
            area.Add(new Point(w, h));

            PolylineSparkline.Points = line;
            PolygonSparklineArea.Points = area;

            string stroke = "AccentBrand";
            if (avg >= 120) stroke = "AccentRed";
            else if (avg >= 40) stroke = "AccentYellow";
            PolylineSparkline.Stroke = (Brush)FindResource(stroke);
        }

        private static Brush Hex(string value)
        {
            return (Brush)new BrushConverter().ConvertFrom(value);
        }

        #endregion

        #region Actions

        private async void BtnOptimize_Click(object sender, RoutedEventArgs e)
        {
            await OptimizeAsync();
        }

        private async Task OptimizeAsync()
        {
            if (_isBusy) return;
            BtnOptimize.IsEnabled = false;
            TxtStatusMsg.Text = "Looking for a better access point...";

            try
            {
                var res = await _agent.CallAsync<OptimizationResult>("optimize", null, 120000);
                if (res == null)
                {
                    TxtStatusMsg.Text = "Could not reach the NomadWiFi core.";
                    return;
                }

                if (res.switched)
                {
                    TxtStatusMsg.Text = "Connected to " + res.target_ssid + ".";
                    TxtRoamCelebrationMsg.Text = "Moved to " + res.target_ssid + " (" + res.band + "). " + res.reason;
                    BorderRoamCelebration.Visibility = Visibility.Visible;
                }
                else if (res.rolled_back)
                {
                    TxtStatusMsg.Text = res.target_ssid + " could not carry traffic, so your previous network was restored.";
                }
                else if (!string.IsNullOrEmpty(res.error))
                {
                    TxtStatusMsg.Text = res.error;
                }
                else
                {
                    TxtStatusMsg.Text = "You are already on the best access point in range.";
                }

                await RefreshAllAsync();
            }
            finally
            {
                BtnOptimize.IsEnabled = true;
            }
        }

        private async void BtnConnect_Click(object sender, RoutedEventArgs e)
        {
            var btn = sender as Button;
            if (btn == null) return;

            var ssid = btn.Tag as string;
            if (string.IsNullOrEmpty(ssid)) return;

            var ap = _currentAps.FirstOrDefault(a =>
                string.Equals(a.ssid, ssid, StringComparison.OrdinalIgnoreCase));

            if (ap != null && ap.IsLocked)
            {
                _pendingModalSsid = ssid;
                TxtModalSsid.Text = ssid;
                BoxPassword.Password = "";
                TxtModalError.Visibility = Visibility.Collapsed;
                OverlayPasswordModal.Visibility = Visibility.Visible;
                BoxPassword.Focus();
                return;
            }

            TxtStatusMsg.Text = "Connecting to " + ssid + "...";
            var response = await _agent.CallAsync("connect",
                new Dictionary<string, object> { { "ssid", ssid } }, 60000);

            TxtStatusMsg.Text = response != null && response.ok
                ? "Connected to " + ssid + "."
                : "Could not connect to " + ssid + (response != null && !string.IsNullOrEmpty(response.error)
                    ? ": " + response.error : ".");

            await RefreshAllAsync();
        }

        private async void BtnSavePassword_Click(object sender, RoutedEventArgs e)
        {
            var password = BoxPassword.Password;
            if (string.IsNullOrEmpty(password) || password.Length < 8)
            {
                TxtModalError.Foreground = (Brush)FindResource("AccentRed");
                TxtModalError.Text = "A Wi-Fi password is at least 8 characters.";
                TxtModalError.Visibility = Visibility.Visible;
                return;
            }

            BtnSavePassword.IsEnabled = false;
            TxtModalError.Foreground = (Brush)FindResource("AccentGreen");
            TxtModalError.Text = "Checking the password...";
            TxtModalError.Visibility = Visibility.Visible;

            var response = await _agent.CallAsync("connect", new Dictionary<string, object>
            {
                { "ssid", _pendingModalSsid },
                { "password", password },
            }, 60000);

            BtnSavePassword.IsEnabled = true;

            if (response != null && response.ok)
            {
                OverlayPasswordModal.Visibility = Visibility.Collapsed;
                TxtStatusMsg.Text = "Connected to " + _pendingModalSsid + ". The network is saved.";
                await RefreshAllAsync();
                return;
            }

            TxtModalError.Foreground = (Brush)FindResource("AccentRed");
            TxtModalError.Text = response != null && !string.IsNullOrEmpty(response.error)
                ? response.error
                : "That password was not accepted.";
        }

        private void BtnCancelPassword_Click(object sender, RoutedEventArgs e)
        {
            OverlayPasswordModal.Visibility = Visibility.Collapsed;
        }

        private void BoxPassword_KeyDown(object sender, KeyEventArgs e)
        {
            if (e.Key == Key.Enter) BtnSavePassword_Click(sender, e);
            else if (e.Key == Key.Escape) BtnCancelPassword_Click(sender, e);
        }

        private void BtnOpenCaptive_Click(object sender, RoutedEventArgs e)
        {
            OpenPortal();
        }

        /// <summary>
        /// Pauses the tunnel, opens the login page, and leaves the VPN for the
        /// user to resume once they are signed in. This is the manual dance
        /// that a kill switch otherwise forces on every hotel check-in.
        /// </summary>
        private async void BtnPauseVpnAndLogin_Click(object sender, RoutedEventArgs e)
        {
            TxtStatusMsg.Text = "Pausing the VPN so the login page can load...";
            await _agent.CallAsync("vpn_hold", null, 30000);
            await Task.Delay(1200);
            OpenPortal();
            await RefreshAllAsync();
            TxtStatusMsg.Text = "Sign in, then press Resume VPN.";
        }

        private async void BtnVpnPause_Click(object sender, RoutedEventArgs e)
        {
            var pausing = string.Equals(BtnVpnPause.Content as string, "Pause VPN",
                StringComparison.OrdinalIgnoreCase);

            await _agent.CallAsync(pausing ? "vpn_hold" : "vpn_resume", null, 30000);
            BtnVpnPause.Content = pausing ? "Resume VPN" : "Pause VPN";
            await RefreshVpnAsync();
        }

        private void OpenPortal()
        {
            try
            {
                Process.Start(new ProcessStartInfo(_captivePortalUrl) { UseShellExecute = true });
            }
            catch (Exception ex)
            {
                TxtStatusMsg.Text = "Could not open the browser: " + ex.Message;
            }
        }

        private void BtnDismissCelebration_Click(object sender, RoutedEventArgs e)
        {
            BorderRoamCelebration.Visibility = Visibility.Collapsed;
        }

        /// <summary>
        /// Reads the stored auto-roam preference from the core. The checkbox
        /// used to be hardcoded on at every launch, so turning roaming off
        /// silently reverted -- which matters on an app that starts with
        /// Windows and changes the network connection.
        /// </summary>
        private async Task SyncAutoRoamAsync()
        {
            var result = await _agent.CallRawAsync("get_autoroam");
            if (result == null) return;

            _isAutoRoamEnabled = ReadBoolField(result, "auto_roam");
            ChkAutoRoam.IsChecked = _isAutoRoamEnabled;
            if (_trayMenuAutoRoam != null) _trayMenuAutoRoam.Checked = _isAutoRoamEnabled;
        }

        #region Updates

        private string _availableUpdate;

        /// <summary>
        /// Asks the core whether a newer release exists. The core throttles the
        /// actual network call to once a day, so calling this on every launch
        /// costs nothing.
        /// </summary>
        private async Task CheckForUpdateAsync()
        {
            var result = await _agent.CallRawAsync("check_update");
            if (result == null) return;

            if (!ReadBoolField(result, "update_available")) return;
            // A version the user already dismissed stays dismissed.
            if (ReadBoolField(result, "dismissed")) return;

            var release = ReadField(result, "release") as Dictionary<string, object>;
            if (release == null) return;

            var version = ReadStringField(release, "version");
            if (string.IsNullOrEmpty(version)) return;

            _availableUpdate = version;
            TxtUpdateMsg.Text = string.Format(
                "NomadWiFi {0} is available. You have {1}.",
                version, ReadStringField(result, "current_version"));
            BorderUpdate.Visibility = Visibility.Visible;
        }

        private async void BtnInstallUpdate_Click(object sender, RoutedEventArgs e)
        {
            BtnInstallUpdate.IsEnabled = false;
            TxtUpdateMsg.Text = "Downloading the update...";

            // The core verifies the download against the published checksum and
            // refuses to run it on a mismatch, so a failure here is safe.
            var result = await _agent.CallRawAsync("install_update");
            if (result == null)
            {
                TxtUpdateMsg.Text = "The update could not be installed. Try again later.";
                BtnInstallUpdate.IsEnabled = true;
                return;
            }

            // The installer closes this app to replace its files and puts it
            // back afterwards, so exiting now makes the handover look clean
            // rather than like a crash.
            TxtUpdateMsg.Text = "Installing. NomadWiFi will restart.";
            await Task.Delay(1200);
            ExitApplication();
        }

        private async void BtnDismissUpdate_Click(object sender, RoutedEventArgs e)
        {
            BorderUpdate.Visibility = Visibility.Collapsed;
            if (!string.IsNullOrEmpty(_availableUpdate))
            {
                // Remembered in the core's state file so the banner does not
                // come back on the next launch for a version already declined.
                await _agent.CallRawAsync("dismiss_update",
                    new Dictionary<string, object> { { "version", _availableUpdate } });
            }
        }

        #endregion

        private async void ChkAutoRoam_Click(object sender, RoutedEventArgs e)
        {
            await SetAutoRoamAsync(ChkAutoRoam.IsChecked == true);
        }

        private async Task SetAutoRoamAsync(bool enabled)
        {
            _isAutoRoamEnabled = enabled;
            ChkAutoRoam.IsChecked = enabled;
            if (_trayMenuAutoRoam != null) _trayMenuAutoRoam.Checked = enabled;

            await _agent.CallAsync("set_autoroam",
                new Dictionary<string, object> { { "enabled", enabled } }, 15000);
        }

        private void ChkStartup_Click(object sender, RoutedEventArgs e)
        {
            var enable = ChkStartup.IsChecked == true;
            StartupManager.SetStartup(enable);
            if (_trayMenuStartup != null) _trayMenuStartup.Checked = enable;
        }

        #endregion
    }
}
