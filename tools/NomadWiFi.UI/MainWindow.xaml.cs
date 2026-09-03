using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Drawing2D;
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

namespace NomadWiFi.UI
{
    public partial class MainWindow : Window
    {
        private readonly NomadCoreClient _client = new NomadCoreClient();
        private readonly DispatcherTimer _pollTimer = new DispatcherTimer();
        private NotifyIcon _notifyIcon;
        private bool _isBusy = false;
        private bool _isExiting = false;

        public MainWindow()
        {
            InitializeComponent();
            SetupSystemTray();

            // Auto-refresh every 5s ONLY when window is visible, not minimized, and active (in foreground view)
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

            Loaded += async (s, e) =>
            {
                await RefreshStatusAsync();
                await RefreshScanAsync();
            };

            // When user switches focus back to NomadWiFi (from browser, editor, etc.)
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

            // Immediate fresh diagnostic on restore into view
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

        #region Wi-Fi Diagnostics & Actions

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
                return;
            }

            TxtSsid.Text = status.ssid;
            TxtDetails.Text = string.Format("{0} • Channel {1} • {2}", status.radio_type, status.channel, status.bssid);

            var is5G = status.band != null && (status.band.Contains("5") || status.band.Contains("6"));
            TxtBand.Text = status.band;
            TxtBand.Foreground = (System.Windows.Media.Brush)FindResource(is5G ? "AccentGreen" : "AccentYellow");

            TxtSignal.Text = string.Format("{0}%", status.signal_percent);
            TxtSpeed.Text = string.Format("{0} Mbps", status.rx_mbps);
            TxtLatency.Text = string.Format("{0:F1} ms", status.gateway_latency_ms);
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

        private async void BtnOptimize_Click(object sender, RoutedEventArgs e)
        {
            if (_isBusy) return;
            _isBusy = true;
            BtnOptimize.IsEnabled = false;
            TxtStatusMsg.Text = "⚡ Optimizing connection...";

            try
            {
                var res = await _client.OptimizeAsync();
                if (res != null && res.success)
                {
                    if (res.switched)
                        TxtStatusMsg.Text = string.Format("🎉 Switched to {0} ({1})!", res.target_ssid, res.band);
                    else
                        TxtStatusMsg.Text = "✅ Already on the optimal access point!";
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

        #endregion
    }
}
