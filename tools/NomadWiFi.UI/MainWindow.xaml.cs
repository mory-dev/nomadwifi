using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;
using NomadWiFi.UI.Models;
using NomadWiFi.UI.Services;

namespace NomadWiFi.UI
{
    public partial class MainWindow : Window
    {
        private readonly NomadCoreClient _client = new NomadCoreClient();
        private readonly DispatcherTimer _pollTimer = new DispatcherTimer();
        private bool _isBusy = false;

        public MainWindow()
        {
            InitializeComponent();

            _pollTimer.Interval = TimeSpan.FromSeconds(4);
            _pollTimer.Tick += async (s, e) => await RefreshStatusAsync();
            _pollTimer.Start();

            Loaded += async (s, e) =>
            {
                await RefreshStatusAsync();
                await RefreshScanAsync();
            };
        }

        private async Task RefreshStatusAsync()
        {
            var status = await _client.GetStatusAsync();
            if (status == null || !status.connected)
            {
                TxtSsid.Text = "Disconnected";
                TxtDetails.Text = "No active Wi-Fi connection detected";
                TxtBand.Text = "Offline";
                TxtBand.Foreground = (Brush)FindResource("AccentRed");
                TxtSignal.Text = "-- %";
                TxtSpeed.Text = "-- Mbps";
                TxtLatency.Text = "-- ms";
                return;
            }

            TxtSsid.Text = status.ssid;
            TxtDetails.Text = string.Format("{0} • Channel {1} • {2}", status.radio_type, status.channel, status.bssid);

            var is5G = status.band != null && (status.band.Contains("5") || status.band.Contains("6"));
            TxtBand.Text = status.band;
            TxtBand.Foreground = (Brush)FindResource(is5G ? "AccentGreen" : "AccentYellow");

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
                TxtStatusMsg.Text = "Scanning nearby networks...";
                var aps = await _client.ScanNetworksAsync();
                ItemsAccessPoints.ItemsSource = aps;
                TxtApCount.Text = string.Format("{0} APs in range", aps.Count);
                TxtStatusMsg.Text = "";
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

        private async void BtnRefresh_Click(object sender, RoutedEventArgs e)
        {
            await RefreshStatusAsync();
            await RefreshScanAsync();
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
                    }
                    else
                    {
                        TxtStatusMsg.Text = string.Format("Failed to connect to {0}.", ssid);
                    }
                }
            }
        }
    }
}


