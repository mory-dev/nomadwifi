using System;
using System.Collections.Generic;
using System.Windows;
using System.Windows.Media;

namespace NomadWiFi.UI.Models
{
    public class InterfaceStatus
    {
        public string interface_name { get; set; }
        public string description { get; set; }
        public bool connected { get; set; }
        public string ssid { get; set; }
        public string bssid { get; set; }
        public string band { get; set; }
        public int channel { get; set; }
        public string radio_type { get; set; }
        public int rx_mbps { get; set; }
        public int tx_mbps { get; set; }
        public int signal_percent { get; set; }
        public int rssi { get; set; }
        public string gateway_ip { get; set; }
        public double gateway_latency_ms { get; set; }
        public double packet_loss_percent { get; set; }
        public bool captive_portal { get; set; }
        public string captive_portal_url { get; set; }

        public bool IsFastBand
        {
            get { return band != null && (band.Contains("5") || band.Contains("6")); }
        }
    }

    public class AccessPoint
    {
        public string ssid { get; set; }
        public string bssid { get; set; }
        public int signal_percent { get; set; }
        public int rssi { get; set; }
        public string band { get; set; }
        public int channel { get; set; }
        public string radio_type { get; set; }
        public string authentication { get; set; }
        public string cipher { get; set; }
        public double quality_score { get; set; }
        public string auth_status { get; set; }
        public string inferred_from { get; set; }
        public bool is_warm { get; set; }
        public int channel_utilization { get; set; }
        public bool has_channel_util { get; set; }
        public int bssid_count { get; set; }
        public List<string> reasons { get; set; }

        public bool IsFastBand
        {
            get { return band != null && (band.Contains("5") || band.Contains("6")); }
        }

        public string ScoreDisplay { get { return quality_score.ToString("F0"); } }

        public string BandTag { get { return band ?? "-"; } }

        public string SignalDisplay
        {
            get
            {
                if (rssi != 0) return string.Format("{0}%  ({1} dBm)", signal_percent, rssi);
                return string.Format("{0}%", signal_percent);
            }
        }

        /// <summary>Shows how many radios broadcast this network, when several do.</summary>
        public string RadioCountDisplay
        {
            get { return bssid_count > 1 ? string.Format("{0} radios", bssid_count) : ""; }
        }

        public Visibility RadioCountVisibility
        {
            get { return bssid_count > 1 ? Visibility.Visible : Visibility.Collapsed; }
        }

        public bool IsLocked
        {
            get { return string.Equals(auth_status, "LOCKED", StringComparison.OrdinalIgnoreCase); }
        }

        /// <summary>The explanation shown on hover: why this network ranked here.</summary>
        public string ReasonTooltip
        {
            get
            {
                var parts = new List<string>();
                if (reasons != null) parts.AddRange(reasons);
                if (has_channel_util)
                {
                    parts.Add(string.Format("access point reports {0}% airtime in use", channel_utilization));
                }
                if (!string.IsNullOrEmpty(inferred_from))
                {
                    parts.Add("password inferred from " + inferred_from);
                }
                if (parts.Count == 0) return "Connect to this network";
                return "Score " + ScoreDisplay + ": " + string.Join(", ", parts.ToArray());
            }
        }

        public string AuthBadgeText
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase))
                    return "Ready";
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase))
                    return is_warm ? "Venue key (ready)" : "Venue key";
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase))
                    return "Open";
                return "Password needed";
            }
        }

        // Note the byte order: WPF parses 8-digit literals as #AARRGGBB,
        // alpha first -- not the CSS #RRGGBBAA. Writing them CSS-style
        // silently yields a different colour rather than an error.
        private static Brush Hex(string value)
        {
            return (Brush)new BrushConverter().ConvertFrom(value);
        }

        public Brush AuthBadgeBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase)) return Hex("#2210B981");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase)) return Hex("#228B5CF6");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase)) return Hex("#223B82F6");
                return Hex("#44283344");
            }
        }

        public Brush AuthBadgeBorderBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase)) return Hex("#10B981");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase)) return Hex("#8B5CF6");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase)) return Hex("#3B82F6");
                return Hex("#4B5563");
            }
        }

        public Brush AuthBadgeTextBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase)) return Hex("#10B981");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase)) return Hex("#C084FC");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase)) return Hex("#60A5FA");
                return Hex("#9CA3AF");
            }
        }

        public Brush BandBrush
        {
            get { return IsFastBand ? Hex("#58A6FF") : Hex("#D29922"); }
        }

        public Visibility WarmVisibility
        {
            get { return is_warm && !IsLocked ? Visibility.Visible : Visibility.Collapsed; }
        }

        public string ConnectBtnText { get { return IsLocked ? "Enter key" : "Connect"; } }
    }

    public class OptimizationResult
    {
        public bool success { get; set; }
        public bool switched { get; set; }
        public string current_ssid { get; set; }
        public string target_ssid { get; set; }
        public string band { get; set; }
        public double score { get; set; }
        public string reason { get; set; }
        public bool rolled_back { get; set; }
        public string error { get; set; }
    }

    public class VpnTunnel
    {
        public string provider { get; set; }
        public string adapter { get; set; }
        public bool up { get; set; }
        public bool owns_default_route { get; set; }
        public bool controllable { get; set; }
        public string control_hint { get; set; }
        public string local_ip { get; set; }
    }

    public class VpnStatus
    {
        public List<VpnTunnel> tunnels { get; set; }
        public VpnTunnel active { get; set; }
    }
}
