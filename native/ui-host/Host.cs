using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.RegularExpressions;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace SplitTunnel.UI {
    internal static class Json {
        internal static readonly JavaScriptSerializer Codec = new JavaScriptSerializer { MaxJsonLength = 4 * 1024 * 1024 };
        internal static Dictionary<string, object> Read(string text) { return Codec.Deserialize<Dictionary<string, object>>(text); }
        internal static string Text(IDictionary<string, object> d, string key, string empty = "") {
            object value; return d != null && d.TryGetValue(key, out value) && value != null ? Convert.ToString(value, CultureInfo.InvariantCulture) : empty;
        }
        internal static bool Flag(IDictionary<string, object> d, string key) { object v; return d != null && d.TryGetValue(key, out v) && v is bool && (bool)v; }
        internal static string[] Strings(IDictionary<string, object> d, string key) {
            object v; if (d == null || !d.TryGetValue(key, out v) || v == null) return new string[0];
            var items = v as System.Collections.IEnumerable;
            return items == null ? new string[0] : items.Cast<object>().Select(x => Convert.ToString(x)).ToArray();
        }
    }

    internal static class Program {
        [STAThread]
        internal static int Main(string[] args) {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.SetUnhandledExceptionMode(UnhandledExceptionMode.ThrowException);
            try {
                if (args.Length != 1) throw new ArgumentException("请通过分流守卫或安装程序打开界面。");
                string input;
                using(var reader = new StreamReader(Console.OpenStandardInput(), new UTF8Encoding(false))) input=reader.ReadToEnd();
                var request = Json.Read(input);
                if (request == null) throw new InvalidDataException("界面启动参数为空。");
                int pid; if (!Int32.TryParse(Json.Text(request, "parent_pid"), out pid)) throw new InvalidDataException("缺少父进程。");
                using (var parent = Process.GetProcessById(pid)) {
                    // Acquire a real process handle now, so later PID reuse cannot keep an orphan alive.
                    var handle = parent.Handle;
                    using (Form form = Create(args[0], request))
                    using (var watch = new Timer { Interval = 500 }) {
                        watch.Tick += delegate { if (parent.HasExited) Environment.Exit(0); };
                        watch.Start();
                        if (form is DashboardForm) form.Shown += delegate { WriteResponse("{\"ready\":true}"); };
                        Application.Run(form);
                        var login = form as LoginForm;
                        if (login != null) WriteResponse(Json.Codec.Serialize(login.Result));
                    }
                }
                return 0;
            } catch (Exception ex) {
                // Never include serialized requests, login results or credentials in diagnostics.
                Console.Error.WriteLine("Native UI failed: " + ex.GetType().Name);
                MessageBox.Show("原生界面无法启动或执行操作。请确认安装完整，或查看程序日志。\n错误类型：" + ex.GetType().Name, "分流守卫", MessageBoxButtons.OK, MessageBoxIcon.Error);
                return 1;
            }
        }
        private static void WriteResponse(string json) {
            using(var writer=new StreamWriter(Console.OpenStandardOutput(),new UTF8Encoding(false))){writer.WriteLine(json);writer.Flush();}
        }
        internal static Form Create(string mode, Dictionary<string, object> request) {
            switch (mode) {
                case "login": return new LoginForm(request);
                case "dashboard": return new DashboardForm(request);
                case "installer": return new InstallerForm(request);
                default: throw new ArgumentException("未知界面模式。");
            }
        }
    }

    internal class Surface : Form {
        internal static readonly Color Ink = Color.FromArgb(13,66,56), Muted = Color.FromArgb(109,125,133), Paper = Color.FromArgb(255,254,248), Cyan = Color.FromArgb(13,135,161), Amber = Color.FromArgb(164,111,27);
        internal readonly string AssetRoot;
        private readonly List<Image> images = new List<Image>();
        internal Surface(string title, Size size, string assets) {
            Text=title; ClientSize=size; AssetRoot=assets; BackColor=Paper; Font=UiFont(10); StartPosition=FormStartPosition.CenterScreen;
            AutoScaleDimensions=new SizeF(96,96); AutoScaleMode=AutoScaleMode.Dpi;
            if (File.Exists(Path.Combine(assets,"app.ico"))) Icon=new Icon(Path.Combine(assets,"app.ico"));
        }
        internal static Font UiFont(float size, bool bold=false) { return new Font("Microsoft YaHei UI",size,bold?FontStyle.Bold:FontStyle.Regular); }
        internal static T Place<T>(Control parent,T control,int x,int y,int w,int h) where T:Control {
            control.SetBounds(x,y,w,h);parent.Controls.Add(control);return control;
        }
        internal static Label Label(Control p,int x,int y,int w,int h,string text,float size=10,bool bold=false,Color? color=null) {
            return Place(p,new Label {Text=text,Font=UiFont(size,bold),ForeColor=color??Ink,BackColor=Color.Transparent},x,y,w,h);
        }
        internal static Button Button(Control p,int x,int y,int w,int h,string text,Action action=null,bool primary=false) {
            var b=Place(p,new Button {Text=text,FlatStyle=FlatStyle.Flat,BackColor=primary?Cyan:Color.FromArgb(235,245,248),ForeColor=primary?Color.White:Ink,Cursor=Cursors.Hand,Font=UiFont(10)},x,y,w,h);
            b.FlatAppearance.BorderSize=0;
            if(action!=null)b.Click+=delegate { try { action(); } catch(Exception ex) { MessageBox.Show(b.FindForm(),ex.Message,"操作未完成",MessageBoxButtons.OK,MessageBoxIcon.Warning); } };
            return b;
        }
        internal Image LoadImage(string name) {
            string path=Path.Combine(AssetRoot,"ui-assets",name);
            if(!File.Exists(path))throw new FileNotFoundException("界面资源缺失，请重新安装。",path);
            using(var file=Image.FromFile(path)){var image=new Bitmap(file);images.Add(image);return image;}
        }
        internal PictureBox Picture(Control parent,string name,int x,int y,int w,int h) {
            return Place(parent,new PictureBox {Image=LoadImage(name),SizeMode=PictureBoxSizeMode.Zoom,BackColor=Color.Transparent},x,y,w,h);
        }
        internal static void Round(Control c,int radius) {
            using(var p=new GraphicsPath()){int w=c.Width-1,h=c.Height-1;p.AddArc(0,0,radius,radius,180,90);p.AddArc(w-radius,0,radius,radius,270,90);p.AddArc(w-radius,h-radius,radius,radius,0,90);p.AddArc(0,h-radius,radius,radius,90,90);p.CloseFigure();var old=c.Region;c.Region=new Region(p);if(old!=null)old.Dispose();}
        }
        internal static void Cue(TextBox box,string text) { SendMessage(box.Handle,0x1501,new IntPtr(1),text); }
        [DllImport("user32.dll",CharSet=CharSet.Unicode)] private static extern IntPtr SendMessage(IntPtr h,int m,IntPtr w,string text);
        protected override void Dispose(bool disposing) {base.Dispose(disposing);if(disposing){foreach(var image in images)image.Dispose();images.Clear();}}
        internal void Contact() { using(var f=new ContactForm(AssetRoot))f.ShowDialog(this); }
        internal void Recharge() { using(var f=new RechargeForm(AssetRoot))f.ShowDialog(this); }
    }

    internal sealed class Target {
        internal string Action, Value;
        internal static Target Parse(string text) {
            string v=(text??"").Trim(); if(v.Length==0 || Regex.IsMatch(v,@"\s"))throw new FormatException("请输入有效的网址、域名或 IP/CIDR。");
            IPAddress address;
            if(IPAddress.TryParse(v,out address))return IP(address,address.AddressFamily==System.Net.Sockets.AddressFamily.InterNetwork?32:128);
            var cidr=Regex.Match(v,@"^([0-9a-fA-F:.]+)/([0-9]{1,3})$");
            if(cidr.Success){if(!IPAddress.TryParse(cidr.Groups[1].Value,out address))throw new FormatException("IP 地址无效。");return IP(address,Int32.Parse(cidr.Groups[2].Value));}
            int slash=v.IndexOf('/'); if(slash>0 && IPAddress.TryParse(v.Substring(0,slash),out address))throw new FormatException("CIDR 前缀长度无效。");
            Uri uri;string url=Regex.IsMatch(v,@"^[a-zA-Z][a-zA-Z0-9+.-]*://")?v:"https://"+v;
            if(!Uri.TryCreate(url,UriKind.Absolute,out uri) || (uri.Scheme!="http"&&uri.Scheme!="https") || uri.UserInfo!="")throw new FormatException("请输入 http(s) 网址、域名或 IP/CIDR。");
            string host=uri.IdnHost.TrimEnd('.').ToLowerInvariant();
            if(IPAddress.TryParse(host.Trim('[',']'),out address))return IP(address,address.AddressFamily==System.Net.Sockets.AddressFamily.InterNetwork?32:128);
            if(!Regex.IsMatch(host,@"^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$"))throw new FormatException("域名格式无效；请检查网址或 IP 地址。");
            return new Target {Action="add_foreign_domain",Value=host};
        }
        private static Target IP(IPAddress ip,int prefix) {
            var b=ip.GetAddressBytes();if(prefix<0||prefix>b.Length*8)throw new FormatException("CIDR 前缀长度无效。");
            for(int i=0;i<b.Length;i++){int bits=Math.Min(8,Math.Max(0,prefix-i*8));b[i]=(byte)(b[i]&(255 << (8-bits)));}
            return new Target {Action="add_foreign_cidr",Value=new IPAddress(b)+"/"+prefix};
        }
    }

    internal sealed class CommandWriter {
        private readonly string directory;
        internal CommandWriter(string dir){directory=dir;}
        internal void Write(string action,object enabled=null,string value=null) {
            var data=new Dictionary<string,object>{{"action",action},{"created_at",DateTime.UtcNow.ToString("o")}};
            if(enabled!=null)data["enabled"]=enabled;if(value!=null)data["value"]=value;
            Directory.CreateDirectory(directory);
            string path=Path.Combine(directory,DateTime.UtcNow.Ticks+"-"+Guid.NewGuid().ToString("N")+".json");
            string temp=path+".tmp";
            try {File.WriteAllText(temp,Json.Codec.Serialize(data),new UTF8Encoding(false));File.Move(temp,path);}
            finally {if(File.Exists(temp))File.Delete(temp);}
        }
    }

    internal sealed class LoginForm : Surface {
        internal readonly ComboBox Sites;
        internal readonly TextBox User,Password;
        internal readonly CheckBox Remember;
        internal readonly Button Connect;
        internal Dictionary<string,object> Result=new Dictionary<string,object>{{"ok",false}};
        private readonly Dictionary<string,string> servers=new Dictionary<string,string>();
        private bool committed;
        internal LoginForm(Dictionary<string,object> request):base("AnyConnect 分流登录",new Size(604,811),Json.Text(request,"asset_root")) {
            FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=false;
            BackgroundImage=LoadImage("desktop-login-bg.png");BackgroundImageLayout=ImageLayout.Stretch;
            var shell=Place(this,new Panel{BackColor=Color.FromArgb(255,253,242)},28,24,544,770);Round(shell,18);
            Place(shell,new Panel{BackColor=Color.FromArgb(112,169,67)},0,0,544,5);
            Picture(shell,"app-brand.png",36,36,54,54);
            Label(shell,116,28,250,18,"VPN ROUTE LOGIN",8,false,Amber);Label(shell,114,48,280,34,"分流守卫",20,true);
            Label(shell,116,84,340,22,"像手机端一样轻松开启云上小路",9,false,Muted);
            Label(shell,438,52,60,22,"● 就绪",9);
            var content=Place(shell,new Panel{BackColor=Color.FromArgb(246,249,240)},28,126,488,226);Round(content,14);
            Label(content,22,18,444,20,"VPN 节点",9,false,Muted);
            Sites=Place(content,new ComboBox{DropDownStyle=ComboBoxStyle.DropDownList,Font=UiFont(10)},22,42,444,30);
            object raw; if(request.TryGetValue("sites",out raw) && raw is System.Collections.IEnumerable)foreach(var entry in (System.Collections.IEnumerable)raw){var site=(Dictionary<string,object>)entry;string name=Json.Text(site,"Name");servers.Add(name,Json.Text(site,"Server"));Sites.Items.Add(name);}
            string preferred=Json.Text(request,"preferred_site");
            if(Sites.Items.Count>0){
                int selected=Sites.Items.Contains(preferred)?Sites.Items.IndexOf(preferred):-1;
                if(selected<0)foreach(string hint in new[]{"香港","台湾","日本","韩国","美国","英国","加拿大","澳大利亚"}){for(int i=0;i<Sites.Items.Count;i++)if(Convert.ToString(Sites.Items[i]).Contains(hint)){selected=i;break;}if(selected>=0)break;}
                Sites.SelectedIndex=selected>=0?selected:0;
            }
            Sites.DropDown+=delegate {committed=false;};Sites.SelectionChangeCommitted+=delegate {committed=true;};
            Sites.DropDownClosed+=delegate {if(!committed&&!IsDisposed&&!Disposing)BeginInvoke((Action)delegate {if(!IsDisposed&&!Disposing&&Visible&&Sites.Enabled){Sites.Focus();Sites.DroppedDown=true;}});};
            Label(content,22,92,180,20,"账号",10,false,Muted);Label(content,256,92,180,20,"密码",10,false,Muted);
            User=Place(content,new TextBox{Text=Json.Text(request,"saved_username"),Font=UiFont(11)},22,116,210,30);
            Password=Place(content,new TextBox{UseSystemPasswordChar=true,Font=UiFont(11)},256,116,210,30);
            Cue(User,"VPN account");Cue(Password,"Password");
            Remember=Place(content,new CheckBox{Text="记住并下次自动连接",Checked=Json.Flag(request,"remember")},22,158,360,24);
            Place(shell,new Panel{BackColor=Color.FromArgb(214,229,190)},28,376,488,1);
            Label(shell,30,394,220,26,"首次使用请先确认账号套餐已开通",8,false,Muted);
            var cancel=Button(shell,256,390,96,44,"取消",delegate {Close();});cancel.DialogResult=DialogResult.Cancel;CancelButton=cancel;
            Connect=Button(shell,368,390,148,44,"连接 VPN",Submit,true);AcceptButton=Connect;
            Button(shell,394,450,122,34,"充值说明",Recharge);
            var contact=Place(shell,new Panel{BackColor=Color.FromArgb(246,249,240)},28,500,488,238);Round(contact,16);
            Label(contact,0,12,488,28,"联系作者",12,true).TextAlign=ContentAlignment.MiddleCenter;
            Label(contact,0,40,488,22,"微信扫码添加作者",9,false,Muted).TextAlign=ContentAlignment.MiddleCenter;
            Picture(contact,"wechat-contact-qr.png",149,62,190,162);
            Shown+=delegate {Activate();if(User.Text.Length==0)User.Focus();else Password.Focus();};
        }
        internal bool Valid(){return Sites.SelectedIndex>=0 && User.Text.Trim().Length>0 && Password.Text.Trim().Length>0;}
        internal void Submit() {
            if(!Valid()){MessageBox.Show(this,"请选择节点，并输入用户名和密码。","需要凭据",MessageBoxButtons.OK,MessageBoxIcon.Warning);return;}
            string name=Convert.ToString(Sites.SelectedItem);
            Result=new Dictionary<string,object>{{"ok",true},{"site_name",name},{"server",servers[name]},{"username",User.Text.Trim()},{"password",Password.Text},{"remember",Remember.Checked}};
            Close();
        }
    }
}
