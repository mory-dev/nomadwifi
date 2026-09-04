using System;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.IO;
using System.Reflection;
using System.Windows.Media.Imaging;

namespace NomadWiFi.UI.Services
{
    /// <summary>Connection quality the tray icon reflects at a glance.</summary>
    public enum TrayState
    {
        Offline,
        Slow,     // connected on a congested or weak link
        Good,     // connected on a fast band
        Roaming,
        VpnHeld,
    }

    /// <summary>
    /// Supplies the application icon and the tinted tray variants.
    ///
    /// The icon is embedded once and reused, rather than drawn with GDI on
    /// every call: the previous code created an HICON per icon and never
    /// destroyed it, which leaked a handle each time.
    /// </summary>
    public static class Branding
    {
        private const string ResourceName = "NomadWiFi.UI.nomadwifi.ico";

        private static readonly object Gate = new object();
        private static byte[] _iconBytes;
        private static Icon _appIcon;
        private static BitmapImage _windowIcon;

        private static byte[] IconBytes()
        {
            lock (Gate)
            {
                if (_iconBytes != null) return _iconBytes;

                var assembly = Assembly.GetExecutingAssembly();
                using (var stream = assembly.GetManifestResourceStream(ResourceName))
                {
                    if (stream == null) return null;
                    using (var memory = new MemoryStream())
                    {
                        stream.CopyTo(memory);
                        _iconBytes = memory.ToArray();
                    }
                }
                return _iconBytes;
            }
        }

        /// <summary>The full-colour application icon.</summary>
        public static Icon AppIcon()
        {
            lock (Gate)
            {
                if (_appIcon != null) return _appIcon;
                var bytes = IconBytes();
                if (bytes == null) return SystemIcons.Application;
                using (var stream = new MemoryStream(bytes))
                {
                    _appIcon = new Icon(stream);
                }
                return _appIcon;
            }
        }

        /// <summary>The window and taskbar icon.</summary>
        public static BitmapImage WindowIcon()
        {
            lock (Gate)
            {
                if (_windowIcon != null) return _windowIcon;
                var bytes = IconBytes();
                if (bytes == null) return null;

                var image = new BitmapImage();
                image.BeginInit();
                image.StreamSource = new MemoryStream(bytes);
                image.CacheOption = BitmapCacheOption.OnLoad;
                image.EndInit();
                image.Freeze();
                _windowIcon = image;
                return _windowIcon;
            }
        }

        private static readonly System.Collections.Generic.Dictionary<TrayState, Icon> TrayCache =
            new System.Collections.Generic.Dictionary<TrayState, Icon>();

        /// <summary>
        /// Returns the tray icon for a connection state, tinted so the state is
        /// readable without opening the window. Icons are cached because a
        /// tray icon is replaced often and each one owns an OS handle.
        /// </summary>
        public static Icon TrayIcon(TrayState state)
        {
            lock (Gate)
            {
                Icon cached;
                if (TrayCache.TryGetValue(state, out cached)) return cached;

                var icon = RenderTrayIcon(state);
                TrayCache[state] = icon;
                return icon;
            }
        }

        private static Color AccentFor(TrayState state)
        {
            switch (state)
            {
                case TrayState.Offline: return Color.FromArgb(0xF8, 0x51, 0x49);
                case TrayState.Slow: return Color.FromArgb(0xD2, 0x99, 0x22);
                case TrayState.Roaming: return Color.FromArgb(0x58, 0xA6, 0xFF);
                case TrayState.VpnHeld: return Color.FromArgb(0xA8, 0x55, 0xF7);
                default: return Color.FromArgb(0x10, 0xB9, 0x81);
            }
        }

        /// <summary>
        /// Draws the logo at tray size with a small status dot in the corner.
        /// Tinting the whole mark would lose the brand, so the state rides
        /// alongside it as a badge instead.
        /// </summary>
        private static Icon RenderTrayIcon(TrayState state)
        {
            IntPtr handle = IntPtr.Zero;

            using (var bmp = new Bitmap(32, 32))
            using (var g = Graphics.FromImage(bmp))
            {
                g.SmoothingMode = SmoothingMode.AntiAlias;
                g.InterpolationMode = InterpolationMode.HighQualityBicubic;
                g.Clear(Color.Transparent);

                using (var source = new Icon(AppIcon(), 32, 32))
                using (var art = source.ToBitmap())
                {
                    g.DrawImage(art, 0, 0, 32, 32);
                }

                // "Good" is the resting state and needs no annotation; every
                // other state earns a badge the user can spot at a glance.
                if (state != TrayState.Good)
                {
                    var accent = AccentFor(state);
                    using (var ring = new SolidBrush(Color.FromArgb(0x0D, 0x11, 0x17)))
                    {
                        g.FillEllipse(ring, 18, 18, 14, 14);
                    }
                    using (var dot = new SolidBrush(accent))
                    {
                        g.FillEllipse(dot, 20, 20, 10, 10);
                    }
                }

                handle = bmp.GetHicon();
                try
                {
                    // Clone so the icon survives destroying the handle we own.
                    using (var temp = Icon.FromHandle(handle))
                    {
                        return (Icon)temp.Clone();
                    }
                }
                finally
                {
                    DestroyIcon(handle);
                }
            }
        }

        [System.Runtime.InteropServices.DllImport("user32.dll", SetLastError = true)]
        private static extern bool DestroyIcon(IntPtr handle);
    }
}
