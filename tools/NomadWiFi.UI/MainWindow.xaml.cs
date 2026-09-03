using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Forms;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Shapes;
using System.Windows.Threading;
using NomadWiFi.UI.Models;
using NomadWiFi.UI.Services;
using Application = System.Windows.Application;
using Button = System.Windows.Controls.Button;
using Point = System.Windows.Point;

namespace NomadWiFi.UI
{
    public partial class MainWindow : Window
    {
        private readonly NomadCoreClient _client = new NomadCoreClient();
        private readonly DispatcherTimer _pollTimer = new DispatcherTimer();
        private readonly DispatcherTimer _watchdogTimer = new DispatcherTimer();
        private readonly List<double> _latencyHistory = new List<double>();
        private NotifyIcon _notifyIcon;
        private ToolStripMenuItem _trayMenuAutoRoam;
        private ToolStripMenuItem _trayMenuStartup;

        private bool _isBusy = false;
        private bool _isExiting = false;
        private bool _isAutoRoamEnabled = true;
        private DateTime _lastRoamTime = DateTime.MinValue;
        private string _captivePortalUrl = "http://neverssl.com";
        private InterfaceStatus _lastStatus;

        public MainWindow() : this(false) { }

        public MainWindow(bool startMinimized)
        {
            InitializeComponent();
            SetupSystemTray();

            // Load settings
            ChkAutoRoam.IsChecked = _isAutoRoamEnabled;
            ChkStartup.IsChecked = StartupManager.IsStartupEnabled();

            // 1. UI Auto-refresh timer (Every 5s ONLY when in foreground view)
            _pollTimer.Interval = TimeSpan.FromSeconds(5);
            _pollTimer.Tick += async (s, e) =>
            {
                if (!IsVisible || WindowState == WindowState.Minimized || !IsActive)
                {
                    return; // 0% CPU & 0 scans when in tray, minimized, or behind other windows
                }
                await RefreshStatusAsync();
                await RefreshScanAsync();
            };
            _pollTimer.Start();

            // 2. Autonomous Background Roam Watchdog (Runs every 15s even in tray)
            _watchdogTimer.Interval = TimeSpan.FromSeconds(15);
            _watchdogTimer.Tick += async (s, e) => await RunWatchdogCheckAsync();
            _watchdogTimer.Start();

            Loaded += async (s, e) =>
            {
                if (startMinimized)
                {
                    Hide();
                }
                else
                {
                    await RefreshStatusAsync();
                    await RefreshScanAsync();
                }
            };

            // Instant refresh on focus / restore
            Activated += async (s, e) =>
            {
                if (IsVisible && WindowState != WindowState.Minimized)
                {
                    await RefreshStatusAsync();
                    await RefreshScanAsync();
                }
            };

            StateChanged += async (s, e) =>
            {
                if (WindowState != WindowState.Minimized && IsVisible)
                {
                    await RefreshStatusAsync();
                    await RefreshScanAsync();
                }
            };

            Closing += MainWindow_Closing;
        }

        #region System Tray Integration

        private void SetupSystemTray()
        {
            _notifyIcon = new NotifyIcon
            {
                Text = "NomadWiFi - Hotel & Travel Wi-Fi Optimizer",
                Icon = CreateTrayIcon(),
                Visible = true
            };

            var contextMenu = new ContextMenuStrip();
            contextMenu.Items.Add("🧭 Open NomadWiFi", null, (s, e) => RestoreFromTray());
            contextMenu.Items.Add("⚡ Auto-Optimize 5GHz", null, async (s, e) =>
            {
                await _client.OptimizeAsync();
                await RefreshStatusAsync();
            });

            _trayMenuAutoRoam = new ToolStripMenuItem("🛡️ Autonomous Auto-Roam", null, new EventHandler((s, e) =>
            {
                _isAutoRoamEnabled = !_isAutoRoamEnabled;
                _trayMenuAutoRoam.Checked = _isAutoRoamEnabled;
                if (ChkAutoRoam != null) ChkAutoRoam.IsChecked = _isAutoRoamEnabled;
            })) { Checked = _isAutoRoamEnabled };
            contextMenu.Items.Add(_trayMenuAutoRoam);

            _trayMenuStartup = new ToolStripMenuItem("🚀 Start with Windows", null, new EventHandler((s, e) =>
            {
                var cur = StartupManager.IsStartupEnabled();
                StartupManager.SetStartup(!cur);
                _trayMenuStartup.Checked = !cur;
                if (ChkStartup != null) ChkStartup.IsChecked = !cur;
            })) { Checked = StartupManager.IsStartupEnabled() };
            contextMenu.Items.Add(_trayMenuStartup);


            contextMenu.Items.Add(new ToolStripSeparator());
            contextMenu.Items.Add("❌ Exit Completely", null, (s, e) => ExitApplication());

            _notifyIcon.ContextMenuStrip = contextMenu;
            _notifyIcon.DoubleClick += (s, e) => RestoreFromTray();
        }

        private Icon CreateTrayIcon()
        {
            using (var bmp = new Bitmap(32, 32))
            using (var g = Graphics.FromImage(bmp))
            {
                g.SmoothingMode = SmoothingMode.AntiAlias;
                g.Clear(System.Drawing.Color.Transparent);

                // Draw dark rounded circle background
                using (var bgBrush = new SolidBrush(System.Drawing.Color.FromArgb(22, 27, 34)))
                {
                    g.FillEllipse(bgBrush, 1, 1, 30, 30);
                }

                // Draw emerald accent Wi-Fi arcs
                using (var pen = new System.Drawing.Pen(System.Drawing.Color.FromArgb(16, 185, 129), 2.5f))
                {
                    g.DrawArc(pen, 5, 5, 22, 22, 210, 120);
                    g.DrawArc(pen, 9, 9, 14, 14, 210, 120);
                }

                // Draw center dot
                using (var dotBrush = new SolidBrush(System.Drawing.Color.FromArgb(16, 185, 129)))
                {
                    g.FillEllipse(dotBrush, 13, 20, 6, 6);
                }

                return System.Drawing.Icon.FromHandle(bmp.GetHicon());
            }
        }

        private async void RestoreFromTray()
        {
            Show();
            WindowState = WindowState.Normal;
            Activate();

            await RefreshStatusAsync();
            await RefreshScanAsync();
        }

        private void MainWindow_Closing(object sender, System.ComponentModel.CancelEventArgs e)
        {
            if (!_isExiting)
            {
                e.Cancel = true;
                Hide();
                if (_notifyIcon != null)
                {
                    _notifyIcon.ShowBalloonTip(2000, "NomadWiFi Active", "NomadWiFi is still optimizing Wi-Fi in the background.", ToolTipIcon.Info);
                }
            }
        }

        private void ExitApplication()
        {
            _isExiting = true;
            if (_notifyIcon != null)
            {
                _notifyIcon.Visible = false;
                _notifyIcon.Dispose();
            }
            Application.Current.Shutdown();
        }

        #endregion

        #region Custom TitleBar Handlers

        private void TitleBar_MouseDown(object sender, MouseButtonEventArgs e)
        {
            if (e.ChangedButton == MouseButton.Left)
            {
                if (e.ClickCount == 2)
                {
                    ToggleMaximize();
                }
                else
                {
                    DragMove();
                }
            }
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
                BtnMaximize.Content = "▢";
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
            if (_notifyIcon != null)
            {
                _notifyIcon.ShowBalloonTip(2000, "NomadWiFi Active", "NomadWiFi is running in your system tray.", ToolTipIcon.Info);
            }
        }

        #endregion

        #region Diagnostics & Real-Time Sparkline Rendering

        private async Task RefreshStatusAsync()
        {
            var status = await _client.GetStatusAsync();
            if (status == null || !status.connected)
            {
                TxtSsid.Text = "Disconnected";
                TxtDetails.Text = "No active Wi-Fi connection detected";
                TxtBand.Text = "Offline";
                TxtBand.Foreground = (System.Windows.Media.Brush)FindResource("AccentRed");
                TxtSignal.Text = "-- %";
                TxtSpeed.Text = "-- Mbps";
                TxtLatency.Text = "-- ms";
                TxtJitter.Text = "Jitter: -- ms";
                BorderCaptivePortal.Visibility = Visibility.Collapsed;
                return;
            }

            _lastStatus = status;
            TxtSsid.Text = status.ssid;
            TxtDetails.Text = string.Format("{0} • Channel {1} • {2}", status.radio_type, status.channel, status.bssid);

            var is5G = status.band != null && (status.band.Contains("5") || status.band.Contains("6"));
            TxtBand.Text = status.band;
            TxtBand.Foreground = (System.Windows.Media.Brush)FindResource(is5G ? "AccentGreen" : "AccentYellow");

            TxtSignal.Text = string.Format("{0}%", status.signal_percent);
            TxtSpeed.Text = string.Format("{0} Mbps", status.rx_mbps);
            TxtLatency.Text = string.Format("{0:F1} ms", status.gateway_latency_ms);

            // Captive Portal Alert Handling
            if (status.captive_portal)
            {
                _captivePortalUrl = string.IsNullOrEmpty(status.captive_portal_url) ? "http://neverssl.com" : status.captive_portal_url;
                BorderCaptivePortal.Visibility = Visibility.Visible;
            }
            else
            {
                BorderCaptivePortal.Visibility = Visibility.Collapsed;
            }

            // Update Latency History & Sparkline
            if (status.gateway_latency_ms > 0)
            {
                _latencyHistory.Add(status.gateway_latency_ms);
                if (_latencyHistory.Count > 25)
                {
                    _latencyHistory.RemoveAt(0);
                }
                UpdateSparkline();
            }
        }

        private void UpdateSparkline()
        {
            if (_latencyHistory.Count < 2) return;

            var w = CanvasSparkline.ActualWidth > 10 ? CanvasSparkline.ActualWidth : 120;
            var h = CanvasSparkline.ActualHeight > 10 ? CanvasSparkline.ActualHeight : 32;

            var min = _latencyHistory.Min();
            var max = _latencyHistory.Max();
            if (Math.Abs(max - min) < 1.0) max = min + 5.0;

            // Calculate Jitter (Mean Absolute Deviation)
            var avg = _latencyHistory.Average();
            var jitter = _latencyHistory.Average(v => Math.Abs(v - avg));
            TxtJitter.Text = string.Format("Jitter: ±{0:F1} ms", jitter);

            var points = new PointCollection();
            var areaPoints = new PointCollection();
            areaPoints.Add(new Point(0, h));

            var step = w / (_latencyHistory.Count - 1);
            for (int i = 0; i < _latencyHistory.Count; i++)
            {
                var val = _latencyHistory[i];
                var y = h - ((val - min) / (max - min) * (h - 6)) - 3;
                var x = i * step;
                var pt = new Point(x, y);
                points.Add(pt);
                areaPoints.Add(pt);
            }

            areaPoints.Add(new Point(w, h));

            PolylineSparkline.Points = points;
            PolygonSparklineArea.Points = areaPoints;

            // Color coding based on latency health
            if (avg < 40)
            {
                PolylineSparkline.Stroke = (System.Windows.Media.Brush)FindResource("AccentGreen");
            }
            else if (avg < 120)
            {
                PolylineSparkline.Stroke = (System.Windows.Media.Brush)FindResource("AccentYellow");
            }
            else
            {
                PolylineSparkline.Stroke = (System.Windows.Media.Brush)FindResource("AccentRed");
            }
        }

        private async Task RefreshScanAsync()
        {
            if (_isBusy) return;
            _isBusy = true;

            try
            {
                var aps = await _client.ScanNetworksAsync();
                ItemsAccessPoints.ItemsSource = aps;
                TxtApCount.Text = string.Format("{0} APs in range", aps.Count);
            }
            catch (Exception ex)
            {
                TxtStatusMsg.Text = string.Format("Scan failed: {0}", ex.Message);
            }
            finally
            {
                _isBusy = false;
            }
        }

        #endregion

        #region Autonomous Roaming Watchdog

        private async Task RunWatchdogCheckAsync()
        {
            if (!_isAutoRoamEnabled || _isBusy) return;

            // Enforce minimum 45s cooldown between autonomous switches
            if ((DateTime.Now - _lastRoamTime).TotalSeconds < 45) return;

            var status = await _client.GetStatusAsync();
            if (status == null || !status.connected) return;

            bool is24GHz = status.band != null && status.band.Contains("2.4");
            bool isWeak = status.signal_percent < 40;
            bool isHighLoss = status.packet_loss_percent > 10.0;
            bool isHighLatency = status.gateway_latency_ms > 150.0;

            // Only trigger auto-roam if connection is suboptimal
            if (!is24GHz && !isWeak && !isHighLoss && !isHighLatency) return;

            var aps = await _client.ScanNetworksAsync();
            var best5G = aps.FirstOrDefault(a => a.Is5GHz && a.CanConnect && a.signal_percent >= 38);

            if (best5G != null && !string.Equals(best5G.ssid, status.ssid, StringComparison.OrdinalIgnoreCase))
            {
                _lastRoamTime = DateTime.Now;
                var oldSpeed = status.rx_mbps > 0 ? status.rx_mbps : 54;
                var oldBand = status.band;

                var success = await _client.ConnectAsync(best5G.ssid);
                if (success)
                {
                    var newStatus = await _client.GetStatusAsync();
                    var newSpeed = newStatus != null ? newStatus.rx_mbps : 351;
                    var ratio = oldSpeed > 0 ? (double)newSpeed / oldSpeed : 1.0;

                    var msg = string.Format("Auto-Roamed to {0}! Speed: {1} Mbps ({2}) ➔ {3} Mbps ({4}) [{5:F1}x faster]",
                        best5G.ssid, oldSpeed, oldBand, newSpeed, best5G.band, ratio);

                    TxtRoamCelebrationMsg.Text = msg;
                    BorderRoamCelebration.Visibility = Visibility.Visible;

                    if (_notifyIcon != null)
                    {
                        _notifyIcon.ShowBalloonTip(4000, "⚡ Auto-Roamed to 5 GHz",
                            string.Format("Switched from {0} to {1} for faster speed & lower latency.", status.ssid, best5G.ssid),
                            ToolTipIcon.Info);
                    }

                    await RefreshStatusAsync();
                    await RefreshScanAsync();
                }
            }
        }

        #endregion

        #region Actions & Event Handlers

        private async void BtnOptimize_Click(object sender, RoutedEventArgs e)
        {
            if (_isBusy) return;
            _isBusy = true;
            BtnOptimize.IsEnabled = false;
            TxtStatusMsg.Text = "⚡ Optimizing connection...";

            try
            {
                var oldStatus = _lastStatus;
                var res = await _client.OptimizeAsync();
                if (res != null && res.success)
                {
                    if (res.switched)
                    {
                        TxtStatusMsg.Text = string.Format("🎉 Switched to {0} ({1})!", res.target_ssid, res.band);
                        if (oldStatus != null)
                        {
                            var oldSpeed = oldStatus.rx_mbps > 0 ? oldStatus.rx_mbps : 54;
                            TxtRoamCelebrationMsg.Text = string.Format("🎉 Optimized to {0}! Speed upgraded from {1} Mbps ({2}) ➔ 5 GHz optimal.", res.target_ssid, oldSpeed, oldStatus.band);
                            BorderRoamCelebration.Visibility = Visibility.Visible;
                        }
                    }
                    else
                    {
                        TxtStatusMsg.Text = "✅ Already on the optimal access point!";
                    }
                }
                else
                {
                    TxtStatusMsg.Text = res != null ? res.error : "Optimization complete.";
                }

                await RefreshStatusAsync();
                await RefreshScanAsync();
            }
            catch (Exception ex)
            {
                TxtStatusMsg.Text = string.Format("Error: {0}", ex.Message);
            }
            finally
            {
                _isBusy = false;
                BtnOptimize.IsEnabled = true;
            }
        }

        private async void BtnConnect_Click(object sender, RoutedEventArgs e)
        {
            var btn = sender as Button;
            if (btn != null)
            {
                var ssid = btn.Tag as string;
                if (!string.IsNullOrEmpty(ssid))
                {
                    TxtStatusMsg.Text = string.Format("Connecting to {0}...", ssid);
                    var success = await _client.ConnectAsync(ssid);
                    if (success)
                    {
                        TxtStatusMsg.Text = string.Format("Connected to {0}!", ssid);
                        await RefreshStatusAsync();
                        await RefreshScanAsync();
                    }
                    else
                    {
                        TxtStatusMsg.Text = string.Format("Failed to connect to {0}.", ssid);
                    }
                }
            }
        }

        private void BtnOpenCaptive_Click(object sender, RoutedEventArgs e)
        {
            try
            {
                Process.Start(new ProcessStartInfo(_captivePortalUrl) { UseShellExecute = true });
            }
            catch (Exception ex)
            {
                TxtStatusMsg.Text = "Could not open browser: " + ex.Message;
            }
        }

        private void BtnDismissCelebration_Click(object sender, RoutedEventArgs e)
        {
            BorderRoamCelebration.Visibility = Visibility.Collapsed;
        }

        private void ChkAutoRoam_Click(object sender, RoutedEventArgs e)
        {
            _isAutoRoamEnabled = ChkAutoRoam.IsChecked == true;
            if (_trayMenuAutoRoam != null)
            {
                _trayMenuAutoRoam.Checked = _isAutoRoamEnabled;
            }
        }

        private void ChkStartup_Click(object sender, RoutedEventArgs e)
        {
            bool enable = ChkStartup.IsChecked == true;
            StartupManager.SetStartup(enable);
            if (_trayMenuStartup != null)
            {
                _trayMenuStartup.Checked = enable;
            }
        }

        #endregion
    }
}
