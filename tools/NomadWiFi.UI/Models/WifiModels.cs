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
        public string gateway_ip { get; set; }
        public double gateway_latency_ms { get; set; }
        public double packet_loss_percent { get; set; }
        public bool captive_portal { get; set; }
        public string captive_portal_url { get; set; }
    }


    public class AccessPoint
    {
        public string ssid { get; set; }
        public string bssid { get; set; }
        public int signal_percent { get; set; }
        public string band { get; set; }
        public int channel { get; set; }
        public string radio_type { get; set; }
        public string authentication { get; set; }
        public string cipher { get; set; }
        public double quality_score { get; set; }
        public string auth_status { get; set; }
        public string inferred_from { get; set; }
        public bool is_warm { get; set; }

        public bool Is5GHz
        {
            get { return band != null && (band.Contains("5") || band.Contains("6")); }
        }

        public string ScoreDisplay
        {
            get { return quality_score.ToString("F0"); }
        }

        public string BandTag
        {
            get { return Is5GHz ? "5 GHz" : "2.4 GHz"; }
        }

        public string SignalDisplay
        {
            get { return string.Format("{0}%", signal_percent); }
        }

        public bool CanConnect
        {
            get { return !string.Equals(auth_status, "LOCKED", StringComparison.OrdinalIgnoreCase); }
        }

        public string AuthBadgeText
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase))
                    return "🟢 Ready";
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase))
                    return "🟣 Hotel Key";
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase))
                    return "🔵 Open";
                return "🔒 Password Req.";
            }
        }

        public Brush AuthBadgeBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#10B98122");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#8B5CF622");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#3B82F622");
                return (Brush)new BrushConverter().ConvertFrom("#28334444");
            }
        }

        public Brush AuthBadgeBorderBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#10B981");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#8B5CF6");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#3B82F6");
                return (Brush)new BrushConverter().ConvertFrom("#4B5563");
            }
        }

        public Brush AuthBadgeTextBrush
        {
            get
            {
                if (string.Equals(auth_status, "SAVED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#10B981");
                if (string.Equals(auth_status, "INFERRED", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#C084FC");
                if (string.Equals(auth_status, "OPEN", StringComparison.OrdinalIgnoreCase))
                    return (Brush)new BrushConverter().ConvertFrom("#60A5FA");
                return (Brush)new BrushConverter().ConvertFrom("#9CA3AF");
            }
        }

        public Visibility WarmVisibility
        {
            get { return is_warm && CanConnect ? Visibility.Visible : Visibility.Collapsed; }
        }

        public string ConnectBtnText
        {
            get { return CanConnect ? "Connect" : "Locked"; }
        }
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
        public string error { get; set; }
    }
}
