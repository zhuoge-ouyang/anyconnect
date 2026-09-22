using System;
using System.Collections.Generic;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Text;
using System.Windows.Forms;
using System.Runtime.InteropServices;
using System.Net.NetworkInformation;

namespace SplitTunnel.UI {
    internal static class NativeTests {
        private static int checks;
        private static void Check(bool yes,string message){checks++;if(!yes)throw new Exception(message);}
        [DllImport("user32.dll")] private static extern bool PrintWindow(IntPtr window,IntPtr dc,uint flags);
        private static void Render(Form form,string path){form.Refresh();Application.DoEvents();using(var bitmap=new Bitmap(form.ClientSize.Width,form.ClientSize.Height)){using(var graphics=Graphics.FromImage(bitmap)){var dc=graphics.GetHdc();try{if(!PrintWindow(form.Handle,dc,1))throw new Exception("PrintWindow failed");}finally{graphics.ReleaseHdc(dc);}}bitmap.Save(path);}}
        private static void TrafficChecks(){
            var s=new TrafficSampler();s.Accept(new TrafficCounter{Id="a",Name="fixture",Received=100,Sent=50},0);
            Check(!s.Available&&s.Samples.Count==0,"first sample must not be a speed");
            s.Accept(new TrafficCounter{Id="a",Name="fixture",Received=3100,Sent=550},2);Check(s.Download==1500&&s.Upload==250,"elapsed-time rate");
            s.Accept(new TrafficCounter{Id="a",Name="fixture",Received=3100,Sent=550},3);Check(s.Available&&s.Download==0,"zero traffic is available");
            s.Accept(new TrafficCounter{Id="b",Name="fixture",Received=900000,Sent=5000},4);Check(!s.Available&&s.Samples.Count==0,"adapter switch baseline");
            s.Accept(new TrafficCounter{Id="b",Name="fixture",Received=5,Sent=1},5);Check(!s.Available,"counter reset");
            s.Accept(new TrafficCounter{Id="b",Name="fixture",Received=10,Sent=2},20);Check(!s.Available,"suspend gap");
            for(int i=21;i<150;i++)s.Accept(new TrafficCounter{Id="b",Name="fixture",Received=i*100,Sent=i*10},i);
            Check(s.Samples.Count==61&&s.Samples.Last().Time-s.Samples.First().Time==60,"bounded 60-second history");
            Check(TrafficSampler.Format(0)=="0 B/s"&&TrafficSampler.Format(1250000)=="1.25 MB/s","speed units");
            s.Reset("unavailable");Check(!s.Available&&s.Samples.Count==0,"error clears stale values");
            using(var panel=new TrafficPanel()){panel.ReadCounter=delegate(int index){throw new NetworkInformationException();};panel.Tick(4);Check(panel.Download.Text=="—"&&panel.Notice.Text.Contains("暂不可用"),"sampling failure must not show zero");}
            var nic=NetworkInterface.GetAllNetworkInterfaces().FirstOrDefault(n=>n.OperationalStatus==OperationalStatus.Up&&n.Supports(NetworkInterfaceComponent.IPv4)&&(n.NetworkInterfaceType==NetworkInterfaceType.Ethernet||n.NetworkInterfaceType==NetworkInterfaceType.Wireless80211));
            if(nic!=null){int index=nic.GetIPProperties().GetIPv4Properties().Index;var a=TrafficReader.Read(index);System.Threading.Thread.Sleep(1100);var b=TrafficReader.Read(index);Check(a.Id==b.Id&&b.Received>=a.Received&&b.Sent>=a.Sent,"live NIC counters");Console.WriteLine("PASS: read-only live NIC sample");}
            else Console.WriteLine("SKIP: no active Ethernet/Wi-Fi interface for live sample");
        }
        [STAThread] public static int Main(string[] args){
            try{
                Application.EnableVisualStyles();Application.SetCompatibleTextRenderingDefault(false);
                TrafficChecks();
                string root=args[0],evidence=args[1];Directory.CreateDirectory(evidence);
                var input=new Dictionary<string,object>{{"asset_root",root},{"snapshot_path",Path.Combine(evidence,"state.json")},{"command_dir",Path.Combine(evidence,"commands")}};
                var state=Json.Read("{\"status_text\":\"状态：TUN 分流已启用\",\"current_site\":\"测试线路\",\"split_mode\":\"domestic_direct\",\"split_tunnel_enabled\":true,\"backend\":\"openconnect_tun\",\"foreign_domains\":[\"example.com\"],\"foreign_cidrs\":[\"203.0.113.8/32\"],\"smart_state\":\"\"}");
                state["sites"]=new[]{"测试线路","日本 | 测试;节点","深圳"};
                File.WriteAllText((string)input["snapshot_path"],Json.Codec.Serialize(state));
                using(var f=new DashboardForm(input)){
                    f.Show();Application.DoEvents();f.Apply(state);
                    Check(f.Status.Text=="已连接"&&f.Disconnect.Visible&&!f.Reconnect.Visible,"connected state");
                    Check(f.Domestic.Checked&&!f.VPN.Checked,"domestic mode");
                    Check(!Directory.Exists((string)input["command_dir"]),"hydration emitted a command");
                    Check(!f.SwitchSite.Enabled,"same site should not reconnect");
                    f.Sites.SelectedItem="日本 | 测试;节点";f.Apply(state);Check(Convert.ToString(f.Sites.SelectedItem)=="日本 | 测试;节点"&&f.SwitchSite.Enabled,"refresh must preserve pending choice");
                    f.ConfirmSite=delegate(string name){return false;};f.SwitchSite.PerformClick();Check(!Directory.Exists((string)input["command_dir"]),"cancel disconnected current site");
                    f.ConfirmSite=delegate(string name){return true;};f.SwitchSite.PerformClick();Check(!f.SwitchSite.Enabled&&!f.Disconnect.Enabled,"switch submission interlock");
                    int submitted=Directory.GetFiles((string)input["command_dir"],"*.json").Length;f.SubmitSite();Check(submitted==Directory.GetFiles((string)input["command_dir"],"*.json").Length,"duplicate switch emitted");
                    state["connection_busy"]=true;f.Apply(state);Check(!f.Sites.Enabled,"backend busy guard");state["connection_busy"]=false;state["current_site"]="日本 | 测试;节点";f.Apply(state);Check(f.Sites.Enabled&&!f.SwitchSite.Enabled,"switch completion");
                    f.QuickInput.Text="https://openapi.longbridge.com/path?q=1";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("openapi.longbridge.com"),"URL add");
                    f.QuickInput.Text="example.com";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("已在"),"duplicate domain");
                    f.QuickInput.Text="203.0.113.9";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("/32"),"IPv4 add");
                    f.QuickInput.Text="https://example.org:8443/path";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("example.org"),"URL port");
                    f.QuickInput.Text="2001:db8::1234/64";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("2001:db8::/64"),"IPv6 normalization");
                    int before=Directory.GetFiles((string)input["command_dir"],"*.json").Length;
                    f.QuickInput.Text="not a URL";f.QuickAdd.PerformClick();Check(Directory.GetFiles((string)input["command_dir"],"*.json").Length==before,"invalid value emitted");
                    f.VPN.Checked=true;Check(f.VPN.Checked,"mode toggle");f.Domestic.Checked=true;
                    f.QuickInput.Clear();f.QuickHint.Text="";state["current_site"]="深圳";f.Apply(state);f.Sites.SelectedItem="深圳";
                    long received=0,sent=0;for(int i=0;i<=60;i++){received+=(long)((1200000+350000*Math.Sin(i*.44))*1);sent+=(long)(300000+80000*Math.Sin(i*.37));f.Traffic.Sampler.Accept(new TrafficCounter{Id="fixture",Name="以太网（隔离示例）",Received=received,Sent=sent},i);}
                    f.Traffic.RenderSample();f.Traffic.Notice.Text="示例数据 · 非当前实际流量";Render(f,Path.Combine(evidence,"dashboard.png"));
                    string reference=Environment.GetEnvironmentVariable("ANYCONNECT_UI_REFERENCE");if(!String.IsNullOrEmpty(reference)){using(var source=Image.FromFile(reference))using(var actual=Image.FromFile(Path.Combine(evidence,"dashboard.png")))using(var both=new Bitmap(actual.Width*2,actual.Height)){using(var g=Graphics.FromImage(both)){g.DrawImage(source,0,0,actual.Width,actual.Height);g.DrawImage(actual,actual.Width,0,actual.Width,actual.Height);}both.Save(Path.Combine(evidence,"comparison.png"));using(var focus=new Bitmap(600,560)){using(var g=Graphics.FromImage(focus)){g.DrawImage(both,new Rectangle(0,0,300,560),new Rectangle(704,80,300,560),GraphicsUnit.Pixel);g.DrawImage(both,new Rectangle(300,0,300,560),new Rectangle(actual.Width+704,80,300,560),GraphicsUnit.Pixel);}focus.Save(Path.Combine(evidence,"comparison-traffic.png"));}}}
                    f.Page(true);Application.DoEvents();Check(f.SettingsPage.Visible&&!f.ConnectPage.Visible,"settings navigation");
                    f.AutoStart.Checked=true;f.UpdateIPDB.PerformClick();f.Send("view_log");
                    Render(f,Path.Combine(evidence,"settings.png"));
                    f.Page(false);state["smart_state"]="running";f.Apply(state);Check(!f.QuickAdd.Enabled&&f.Smart.Text=="取消智能选线","smart selection interlock");
                    state["smart_state"]="";state["status_text"]="状态：VPN 未连接";f.Apply(state);Check(f.Reconnect.Visible&&!f.Disconnect.Visible,"disconnected state");
                    state["status_text"]="状态：正在连接";f.Apply(state);Check(!f.Reconnect.Enabled&&f.Status.Text=="连接处理中","busy state");
                    File.WriteAllText((string)input["snapshot_path"],"invalid");f.RefreshState();Check(f.Latest==null&&!f.QuickAdd.Enabled&&f.Status.Text=="状态不可用","invalid snapshot guard");
                    File.WriteAllText((string)input["snapshot_path"],Json.Codec.Serialize(state));f.RefreshState();Check(f.Latest!=null,"snapshot recovery");
                    state["status_text"]="状态：TUN 分流已启用";f.Apply(state);f.ClientSize=new Size(900,740);Application.DoEvents();Check(f.ConnectPage.Right<=f.ConnectPage.Parent.ClientSize.Width&&f.SwitchSite.Right<=f.ConnectPage.ClientSize.Width,"minimum layout");Render(f,Path.Combine(evidence,"narrow.png"));
                    f.ClientSize=new Size(1488,1058);Application.DoEvents();Check(f.Traffic.Right==f.ClientSize.Width&&f.SwitchSite.Right<=f.ConnectPage.ClientSize.Width,"large layout");Render(f,Path.Combine(evidence,"large.png"));
                    f.Close();
                }
                foreach(var pair in new[]{new[]{"203.0.113.42/24","203.0.113.0/24"},new[]{"https://[2001:db8::1]/p","2001:db8::1/128"},new[]{"EXAMPLE.COM/path","example.com"}})Check(Target.Parse(pair[0]).Value==pair[1],"normalization "+pair[0]);
                foreach(var bad in new[]{"203.0.113.8/33","2001:db8::/129","ftp://example.com","https://user:pass@example.com","invalid","a b.com"}){bool rejected=false;try{Target.Parse(bad);}catch(FormatException){rejected=true;}Check(rejected,"accepted bad target");}
                var writer=new CommandWriter((string)input["command_dir"]);
                using(var f=new WhitelistForm(root,writer,state)){
                    f.Show();Application.DoEvents();f.Input.Text="https://api.example.com/path";f.AddDomain.PerformClick();Check(f.Items.Items.Contains("域名 | api.example.com"),"whitelist domain");
                    f.Input.Text="203.0.113.99/24";f.AddIP.PerformClick();Check(f.Items.Items.Contains("IP/CIDR | 203.0.113.0/24"),"whitelist CIDR");
                    f.Items.SelectedIndex=f.Items.Items.Count-1;f.Remove.PerformClick();Check(!f.Items.Items.Contains("IP/CIDR | 203.0.113.0/24"),"whitelist remove");
                    Render(f,Path.Combine(evidence,"whitelist.png"));f.Close();
                }
                input["sites"]=new object[]{new Dictionary<string,object>{{"Name","深圳 | 测试;节点"},{"Server","https://example.invalid:443"}},new Dictionary<string,object>{{"Name","日本"},{"Server","https://jp.example.invalid"}}};
                input["preferred_site"]="深圳 | 测试;节点";input["saved_username"]="测试用户";input["remember"]=true;
                using(var f=new LoginForm(input)){
                    f.Show();Application.DoEvents();Check(Convert.ToString(f.Sites.SelectedItem)=="深圳 | 测试;节点","site names must not use delimiters");
                    f.Sites.DroppedDown=true;Application.DoEvents();f.Sites.DroppedDown=false;Application.DoEvents();Check(f.Sites.DroppedDown,"dropdown closes without committed selection");
                    typeof(ComboBox).GetMethod("OnSelectionChangeCommitted",System.Reflection.BindingFlags.Instance|System.Reflection.BindingFlags.NonPublic).Invoke(f.Sites,new object[]{EventArgs.Empty});
                    f.Sites.DroppedDown=false;Application.DoEvents();Check(!f.Sites.DroppedDown,"dropdown reopens after committed selection");
                    Check(f.Password.UseSystemPasswordChar&&!f.Valid(),"password masking and blank validation");
                    f.Password.Text="test-only-secret";Check(f.Valid(),"valid login");Render(f,Path.Combine(evidence,"login.png"));
                    f.Connect.PerformClick();Check(Json.Flag(f.Result,"ok")&&Json.Text(f.Result,"server")=="https://example.invalid:443"&&Json.Text(f.Result,"password")=="test-only-secret","login response");
                }
                using(var f=new LoginForm(input)){f.Show();Application.DoEvents();f.Close();Check(!Json.Flag(f.Result,"ok"),"cancelled login");}
                using(var f=new ContactForm(root)){f.Show();Application.DoEvents();Render(f,Path.Combine(evidence,"contact.png"));f.Close();}
                using(var f=new RechargeForm(root)){f.Show();Application.DoEvents();Check(f.Controls.OfType<TextBox>().Single().Text.Contains("【六、安装其他设备】"),"complete recharge text");f.Close();}
                state["smart_candidate"]="测试推荐线路";state["smart_attempts"]=3;state["smart_successes"]=3;
                using(var f=new RecommendationForm(root,writer,state)){f.Show();Application.DoEvents();Check(!f.ControlBox,"recommendation close bypass");f.Controls.OfType<Button>().Single(b=>b.Text=="立即采用").PerformClick();}
                using(var f=new RecommendationForm(root,writer,state)){f.Show();Application.DoEvents();f.Controls.OfType<Button>().Single(b=>b.Text=="恢复原线路").PerformClick();}
                var setup=new Dictionary<string,object>{{"asset_root",root},{"installer_path",System.Diagnostics.Process.GetCurrentProcess().MainModule.FileName},{"default_install_dir",@"C:\Program Files\AnyConnectSplitTunnel"}};
                using(var f=new InstallerForm(setup)){f.Show();Application.DoEvents();Render(f,Path.Combine(evidence,"installer.png"));f.Close();}
                Check(!Directory.GetFiles((string)input["command_dir"],"*.tmp").Any(),"partial command file remained");
                var commands=Directory.GetFiles((string)input["command_dir"],"*.json").Select(p=>Json.Read(File.ReadAllText(p))).ToArray();
                foreach(var action in new[]{"select_site","add_foreign_domain","add_foreign_cidr","remove_foreign_cidr","set_split_mode","toggle_autostart","update_ipdb","view_log","smart_select_accept","smart_select_restore"})Check(commands.Any(c=>Json.Text(c,"action")==action),"missing action "+action);
                Check(commands.Count(c=>Json.Text(c,"action")=="select_site")==1&&Json.Text(commands.Single(c=>Json.Text(c,"action")=="select_site"),"value")=="日本 | 测试;节点","selected site command value");
                Console.WriteLine("PASS: "+checks+" native WinForms checks");return 0;
            }catch(Exception ex){Console.Error.WriteLine(ex);return 1;}
        }
    }
}
