using System;
using System.Collections.Generic;
using System.Drawing;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Text.RegularExpressions;
using System.Windows.Forms;

namespace SplitTunnel.UI {
    internal sealed class DashboardForm : Surface {
        internal readonly CommandWriter Commands;
        internal Dictionary<string,object> Latest;
        internal readonly Label Status,SiteLabel,Hint,QuickHint,ModeHint;
        internal readonly Button Disconnect,Reconnect,QuickAdd,Rules,Smart,Restore,UpdateIPDB,NavRules;
        internal readonly TextBox QuickInput;
        internal readonly RadioButton Domestic,VPN;
        internal readonly CheckBox AutoStart;
        internal readonly Panel ConnectPage,SettingsPage;
        internal readonly ComboBox Sites;
        internal readonly Button SwitchSite;
        internal readonly TrafficPanel Traffic;
        internal Func<string,bool> ConfirmSite;
        private string pendingSite="";
        private DateTime pendingSince;
        private bool switching;
        private readonly Panel sidebar,workspace,footer,underline;
        private readonly Label brandFooter,brandTitle,brandTagline;
        private readonly Dictionary<string,Label> diagnostics=new Dictionary<string,Label>();
        private readonly string snapshot;
        private readonly Timer timer=new Timer{Interval=1000};
        private bool hydrating;
        private string lastResult="";
        internal DashboardForm(Dictionary<string,object> request):base("AnyConnect 分流管理台",new Size(1008,716),Json.Text(request,"asset_root")) {
            snapshot=Json.Text(request,"snapshot_path");Commands=new CommandWriter(Json.Text(request,"command_dir"));
            MinimumSize=new Size(900,720);
            sidebar=Place(this,new Panel{BackColor=Paper},0,0,242,716);
            Image art=LoadImage("desktop-dashboard-sidebar.png");
            sidebar.Paint+=delegate(object sender,PaintEventArgs e){double scale=Math.Max((double)sidebar.Width/art.Width,(double)sidebar.Height/art.Height);int w=(int)Math.Ceiling(art.Width*scale),h=(int)Math.Ceiling(art.Height*scale);e.Graphics.DrawImage(art,(sidebar.Width-w)/2,(sidebar.Height-h)/2,w,h);};
            Picture(sidebar,"app-brand.png",26,82,54,54);brandTitle=Label(sidebar,92,80,160,42,"分流守卫",22,true);
            brandTagline=Label(sidebar,94,121,160,24,"国内直连，按需连接",9);
            brandFooter=Label(sidebar,26,628,210,60,"更自由的连接\n始于合适的分流",10,false,Color.White);
            workspace=Place(this,new Panel{BackColor=Paper},280,0,690,716);
            Button(workspace,0,12,70,34,"连接",delegate{Page(false);}).BackColor=Paper;
            NavRules=Button(workspace,80,12,106,34,"分流规则",Whitelist);NavRules.BackColor=Paper;
            Button(workspace,196,12,70,34,"设置",delegate{Page(true);}).BackColor=Paper;
            foreach(var button in workspace.Controls.OfType<Button>())button.Font=UiFont(11);
            underline=Place(workspace,new Panel{BackColor=Cyan},11,50,48,2);
            ConnectPage=Place(workspace,new Panel{BackColor=Paper,AutoScroll=true},0,72,690,540);
            SettingsPage=Place(workspace,new Panel{BackColor=Paper,AutoScroll=true,Visible=false},0,72,690,540);
            Status=Label(ConnectPage,0,44,660,64,"正在读取",34,true);SiteLabel=Label(ConnectPage,0,116,660,36,"当前线路：未连接",15,false,Muted);
            Sites=Place(ConnectPage,new ComboBox{DropDownStyle=ComboBoxStyle.DropDownList,Font=UiFont(11),AccessibleName="选择 VPN 站点",Anchor=AnchorStyles.Top|AnchorStyles.Left|AnchorStyles.Right},0,158,268,34);
            SwitchSite=Button(ConnectPage,276,155,112,38,"切换站点",SubmitSite);SwitchSite.Anchor=AnchorStyles.Top|AnchorStyles.Right;SwitchSite.BackColor=Paper;
            ConfirmSite=delegate(string name){return MessageBox.Show(this,"切换到「"+name+"」？\n确认后 VPN 将短暂断开并重新连接。取消不会影响当前连接。","切换站点",MessageBoxButtons.YesNo,MessageBoxIcon.Question,MessageBoxDefaultButton.Button2)==DialogResult.Yes;};
            Sites.SelectedIndexChanged+=delegate{if(!hydrating)UpdateSiteEnabled();};
            Disconnect=Button(ConnectPage,0,210,228,52,"断开连接",delegate{Send("disconnect");},true);
            Reconnect=Button(ConnectPage,0,210,228,52,"连接 VPN",delegate{Send("reconnect");},true);
            Disconnect.Font=UiFont(14,true);Reconnect.Font=UiFont(14,true);Disconnect.Visible=false;Reconnect.Enabled=false;
            Separator(ConnectPage,286);Label(ConnectPage,0,306,660,26,"连接模式",12,true);
            Domestic=Place(ConnectPage,new RadioButton{Text="国内直连",Font=UiFont(12),ForeColor=Ink,Enabled=false},0,342,140,32);
            VPN=Place(ConnectPage,new RadioButton{Text="VPN 优先",Font=UiFont(12),ForeColor=Ink,Enabled=false},144,342,140,32);
            ModeHint=Label(ConnectPage,0,382,680,42,"默认直连，仅国外白名单走 VPN。",10,false,Muted);
            Separator(ConnectPage,418);Label(ConnectPage,0,434,660,30,"需要单独走 VPN？",12,true);
            var inputHost=Place(ConnectPage,new Panel{BackColor=Color.White,BorderStyle=BorderStyle.FixedSingle,Anchor=AnchorStyles.Top|AnchorStyles.Left|AnchorStyles.Right},0,468,548,42);
            QuickInput=Place(inputHost,new TextBox{BorderStyle=BorderStyle.None,Font=UiFont(11),AccessibleName="网址、域名或 IP/CIDR",Anchor=AnchorStyles.Top|AnchorStyles.Left|AnchorStyles.Right},10,10,524,24);Cue(QuickInput,"粘贴网址、域名或 IP/CIDR");
            QuickAdd=Button(ConnectPage,564,468,126,42,"添加",SubmitQuick);QuickAdd.Anchor=AnchorStyles.Top|AnchorStyles.Right;QuickAdd.Enabled=false;
            Rules=Button(ConnectPage,0,518,132,34,"查看全部规则",Whitelist);Rules.BackColor=Paper;
            QuickHint=Label(ConnectPage,0,550,680,20,"",9,false,Muted);QuickHint.AutoEllipsis=true;
            QuickInput.KeyDown+=delegate(object sender,KeyEventArgs e){if(e.KeyCode==Keys.Enter){e.SuppressKeyPress=true;if(QuickAdd.Enabled)SubmitQuick();}};
            Label(SettingsPage,0,16,660,48,"设置与诊断",23,true);
            AutoStart=Place(SettingsPage,new CheckBox{Text="登录 Windows 时启动",ForeColor=Ink},0,74,300,30);
            Smart=Button(SettingsPage,0,120,260,40,"ChatGPT/Codex 智能选线",SmartClick);
            Restore=Button(SettingsPage,0,120,260,40,"恢复常用线路",delegate{Send("restore_normal");});Restore.Visible=false;
            UpdateIPDB=Button(SettingsPage,274,120,172,40,"更新 IP 数据库",delegate{Send("update_ipdb");});
            Button(SettingsPage,460,120,128,40,"退出程序",delegate{Send("quit");});
            int y=188;foreach(string name in new[]{"分流模式","后端模式","路由数量","IP 库更新","IPv4 网关","IPv6 网关","IPv6 分流","最近错误"}){Label(SettingsPage,0,y,100,26,name,9,false,Muted);diagnostics[name]=Label(SettingsPage,120,y,542,26,"读取中...",9);y+=34;}
            Button(SettingsPage,0,486,126,38,"充值说明",Recharge);Button(SettingsPage,142,486,126,38,"联系作者",Contact);
            footer=Place(workspace,new Panel{BackColor=Paper},0,628,690,80);Separator(footer,0);
            Hint=Label(footer,0,20,510,32,"正在读取连接状态…",10,false,Muted);
            Hint.Font=UiFont(9);
            var log=Button(footer,574,14,116,34,"查看日志",delegate{Send("view_log");});log.BackColor=Paper;log.Anchor=AnchorStyles.Top|AnchorStyles.Right;
            Traffic=Place(this,new TrafficPanel(),694,0,314,716);
            foreach(var panel in new[]{ConnectPage,SettingsPage,footer})foreach(Control c in panel.Controls)if(c is Label)c.Anchor=AnchorStyles.Top|AnchorStyles.Left|AnchorStyles.Right;
            AutoStart.CheckedChanged+=delegate{if(!hydrating)Send("toggle_autostart",AutoStart.Checked);};
            Domestic.CheckedChanged+=delegate{if(!hydrating&&Domestic.Checked)Send("set_split_mode",null,"domestic_direct");};
            VPN.CheckedChanged+=delegate{if(!hydrating&&VPN.Checked)Send("set_split_mode",null,"foreign_direct");};
            Resize+=delegate{ResizeContent();};ResizeContent();Page(false);
            timer.Tick+=delegate{RefreshState();SampleTraffic();};Shown+=delegate{RefreshState();SampleTraffic();timer.Start();Activate();};FormClosed+=delegate{timer.Stop();timer.Dispose();};
        }
        private static void Separator(Control p,int y){Place(p,new Panel{BackColor=Color.FromArgb(227,233,226),Anchor=AnchorStyles.Top|AnchorStyles.Left|AnchorStyles.Right},0,y,690,1);}
        private void ResizeContent(){
            if(workspace==null||footer==null||Traffic==null)return;float dpi=DeviceScale();int sw=(int)(ClientSize.Width*.24),rail=(int)(ClientSize.Width*.31);
            sidebar.SetBounds(0,0,sw,ClientSize.Height);brandFooter.Top=ClientSize.Height-(int)(88*dpi);
            float brandSize=sw/dpi<235?18:22;if(Math.Abs(brandTitle.Font.Size-brandSize)>.1){var old=brandTitle.Font;brandTitle.Font=UiFont(brandSize,true);old.Dispose();}
            brandTitle.Width=sw-brandTitle.Left;brandTagline.Left=(int)((sw/dpi<235?26:94)*dpi);brandTagline.Width=sw-brandTagline.Left;
            brandTagline.Top=(int)((sw/dpi<235?144:121)*dpi);
            bool settings=SettingsPage.Visible;Traffic.Visible=!settings;Traffic.SetBounds(ClientSize.Width-rail,0,rail,ClientSize.Height);Traffic.LayoutAt(dpi);
            int right=settings?ClientSize.Width:Traffic.Left;
            workspace.SetBounds(sw+(int)(38*dpi),0,right-sw-(int)(64*dpi),ClientSize.Height);
            int content=Math.Max((int)(420*dpi),workspace.Height-(int)(146*dpi));
            foreach(var p in new[]{ConnectPage,SettingsPage})p.SetBounds(0,(int)(72*dpi),workspace.Width,content);
            ConnectPage.AutoScrollMinSize=new Size(0,(int)(570*dpi));
            foreach(Control c in ConnectPage.Controls)if(c is Label || (c is Panel && c.Height<=2*dpi))c.Width=Math.Max(20,ConnectPage.ClientSize.Width-c.Left);
            Sites.Width=Math.Max((int)(120*dpi),ConnectPage.ClientSize.Width-(int)(110*dpi));SwitchSite.Left=Sites.Right+(int)(4*dpi);SwitchSite.Width=(int)(106*dpi);
            QuickAdd.Width=(int)(92*dpi);QuickAdd.Left=ConnectPage.ClientSize.Width-QuickAdd.Width;
            QuickInput.Parent.Width=Math.Max((int)(110*dpi),QuickAdd.Left-(int)(8*dpi));QuickInput.Width=QuickInput.Parent.ClientSize.Width-(int)(20*dpi);
            footer.SetBounds(0,workspace.Height-(int)(72*dpi),workspace.Width,(int)(68*dpi));Hint.Width=Math.Max(50,footer.Width-(int)(116*dpi));
            foreach(var button in new[]{Disconnect,Reconnect,QuickAdd})Round(button,(int)(8*dpi));
            sidebar.Invalidate();
        }
        private float DeviceScale(){using(var g=CreateGraphics())return g.DpiX/96f;}
        internal void Page(bool settings){SettingsPage.Visible=settings;ConnectPage.Visible=!settings;underline.Left=(int)((settings?207:11)*DeviceScale());ResizeContent();}
        internal void SampleTraffic(){if(Latest==null){Traffic.Unavailable();return;}int index;Int32.TryParse(Json.Text(Latest,"original_interface"),out index);if(index<=0)Int32.TryParse(Json.Text(Latest,"original_ipv6_interface_index"),out index);Traffic.Tick(index);}
        private void UpdateSiteEnabled(){string chosen=Convert.ToString(Sites.SelectedItem);SwitchSite.Enabled=Latest!=null&&!switching&&!Json.Flag(Latest,"connection_busy")&&Sites.Enabled&&chosen!=""&&(!Disconnect.Visible||chosen!=Json.Text(Latest,"current_site"));}
        internal void SubmitSite(){
            if(!SwitchSite.Enabled||Latest==null)return;string chosen=Convert.ToString(Sites.SelectedItem);
            if(!Json.Strings(Latest,"sites").Contains(chosen)||!ConfirmSite(chosen))return;
            Commands.Write("select_site",null,chosen);pendingSite=chosen;pendingSince=DateTime.UtcNow;switching=true;Sites.Enabled=SwitchSite.Enabled=false;
            Disconnect.Enabled=Reconnect.Enabled=false;Hint.Text="正在切换到 "+chosen+"…";
        }
        internal void Send(string action,object enabled=null,string value=null){try{Commands.Write(action,enabled,value);Hint.Text="操作已提交，等待程序处理…";}catch(Exception ex){Hint.Text="操作提交失败："+ex.Message;Hint.ForeColor=Amber;}}
        internal void SubmitQuick(){try{if(Latest==null)throw new InvalidOperationException("状态不可用，暂不能添加规则。");var t=Target.Parse(QuickInput.Text);string field=t.Action=="add_foreign_cidr"?"foreign_cidrs":"foreign_domains";if(Json.Strings(Latest,field).Contains(t.Value)){QuickHint.Text="该目标已在白名单中。";return;}Commands.Write(t.Action,null,t.Value);QuickHint.ForeColor=Muted;QuickHint.Text="已提交："+t.Value;QuickInput.Clear();}catch(Exception ex){QuickHint.ForeColor=Color.Firebrick;QuickHint.Text=ex.Message;}}
        private void Whitelist(){if(Latest==null)return;using(var f=new WhitelistForm(AssetRoot,Commands,Latest))f.ShowDialog(this);}
        private void SmartClick(){string s=Json.Text(Latest,"smart_state");if(s=="running"||s=="restoring"){Send("smart_select_cancel");return;}if(MessageBox.Show(this,"智能诊断最多 60 秒。检测期间国外连接和 ChatGPT 响应可能短暂中断，国内直连应用通常不受影响。是否开始？","开始智能选线",MessageBoxButtons.YesNo,MessageBoxIcon.Warning,MessageBoxDefaultButton.Button2)==DialogResult.Yes)Send("codex_mode");}
        internal void Unavailable(string reason){Latest=null;Status.Text="状态不可用";SiteLabel.Text="当前线路：未确认";Disconnect.Visible=false;Reconnect.Visible=true;foreach(Control c in new Control[]{Sites,SwitchSite,Reconnect,Smart,Restore,UpdateIPDB,Rules,NavRules,Domestic,VPN,QuickAdd,AutoStart})c.Enabled=false;Traffic.Unavailable();Hint.Text=reason;Hint.ForeColor=Amber;}
        internal void RefreshState(){try{var state=Json.Read(File.ReadAllText(snapshot));if(state==null||!state.ContainsKey("status_text"))throw new InvalidDataException();Apply(state);}catch(IOException){Unavailable("状态读取失败，请查看日志。");}catch(UnauthorizedAccessException){Unavailable("无法读取状态文件，请检查权限。");}catch(ArgumentException){Unavailable("状态格式无效，请查看日志。");}catch(InvalidOperationException){Unavailable("状态格式无效，请查看日志。");}}
        internal void Apply(Dictionary<string,object> state){
            hydrating=true;Latest=state;
            try{
                string status=Json.Text(state,"status_text","状态：未知");bool busy=Json.Flag(state,"connection_busy")||Regex.IsMatch(status,"正在|初始化|检测中"),connected=Regex.IsMatch(status,"已启用|已连接|已回退静态路由")&&!Regex.IsMatch(status,"未连接|已断开|失败|错误");
                Status.Text=busy?"连接处理中":Regex.IsMatch(status,"未连接|已断开")?"未连接":Json.Text(state,"last_error")!=""?"连接异常":connected?"已连接":status.Replace("状态：","");
                Disconnect.Visible=connected;Reconnect.Visible=!connected;SiteLabel.Text="当前线路："+Json.Text(state,"current_site","未连接");
                Hint.Text=connected&&!status.Contains("分流未启用")?"连接正常 · 分流已启用":status.Replace("状态：","");Hint.ForeColor=connected?Color.FromArgb(36,126,77):Muted;
                string smart=Json.Text(state,"smart_state");bool running=new[]{"running","restoring","current_healthy","recommendation"}.Contains(smart);
                if(switching&&(busy||Json.Text(state,"current_site")==pendingSite||Json.Text(state,"last_error")!=""||DateTime.UtcNow-pendingSince>TimeSpan.FromSeconds(5))){switching=false;pendingSite="";}
                busy=busy||switching;
                string selected=Convert.ToString(Sites.SelectedItem);var names=Json.Strings(state,"sites");
                if(!names.SequenceEqual(Sites.Items.Cast<string>())){Sites.Items.Clear();Sites.Items.AddRange(names);Sites.SelectedItem=names.Contains(selected)?selected:Json.Text(state,"current_site");}
                Sites.DropDownWidth=Math.Min(Screen.FromControl(this).WorkingArea.Width/2,Math.Max(Sites.Width,names.Length==0?Sites.Width:names.Max(n=>TextRenderer.MeasureText(n,Sites.Font).Width)+32));
                if(Sites.SelectedIndex<0&&Sites.Items.Count>0)Sites.SelectedIndex=0;
                Sites.Enabled=!busy&&!running&&Sites.Items.Count>0;
                Disconnect.Enabled=Reconnect.Enabled=!running&&!busy;foreach(Control c in new Control[]{Restore,UpdateIPDB,Rules,NavRules,Domestic,VPN,QuickAdd})c.Enabled=!running&&!busy;
                bool codex=Json.Flag(state,"codex_mode_active");Smart.Visible=!codex;Restore.Visible=codex;
                if(smart=="running"||smart=="restoring"){Smart.Visible=true;Restore.Visible=false;Smart.Text="取消智能选线";}else Smart.Text="ChatGPT/Codex 智能选线";
                Smart.Enabled=!busy||running;AutoStart.Enabled=!busy;AutoStart.Checked=Json.Flag(state,"auto_start_enabled");bool vpn=Json.Text(state,"split_mode")=="foreign_direct";Domestic.Checked=!vpn;VPN.Checked=vpn;
                ModeHint.Text=vpn?"默认通过 VPN；国内白名单保持直连。":"默认直连，仅国外白名单走 VPN。";
                diagnostics["分流模式"].Text=(Json.Flag(state,"split_tunnel_enabled")?"已启用 / ":"未启用 / ")+(vpn?"国外 VPN 优先":"国内直连优先");
                diagnostics["后端模式"].Text=Json.Text(state,"backend","未连接");diagnostics["路由数量"].Text=Json.Text(state,"route_count","0");
                DateTime dt;diagnostics["IP 库更新"].Text=DateTime.TryParse(Json.Text(state,"last_ipdb_update"),out dt)&&dt.Year>=2000?dt.ToLocalTime().ToString("yyyy-MM-dd HH:mm"):"未更新";
                diagnostics["IPv4 网关"].Text=Gateway(state,"original_gateway","original_interface");diagnostics["IPv6 网关"].Text=Gateway(state,"original_ipv6_gateway","original_ipv6_interface_index");
                diagnostics["IPv6 分流"].Text=Json.Flag(state,"ipv6_split_enabled")?"已启用":"未启用";diagnostics["最近错误"].Text=Json.Text(state,"last_error")==""?"无":Json.Text(state,"last_error");
            }finally{hydrating=false;UpdateSiteEnabled();}
            string id=Json.Text(state,"smart_result_id"),step=Json.Text(state,"smart_state");
            if(id!=""&&id!=lastResult){lastResult=id;timer.Stop();try{if(step=="current_healthy")Send(MessageBox.Show(this,Json.Text(state,"smart_message")+"\n\n是否继续深度检测其他线路？","当前线路健康",MessageBoxButtons.YesNo,MessageBoxIcon.Information)==DialogResult.Yes?"smart_select_continue":"smart_select_cancel");else if(step=="recommendation")using(var f=new RecommendationForm(AssetRoot,Commands,state))f.ShowDialog(this);}finally{if(!IsDisposed)timer.Start();}}
        }
        private static string Gateway(Dictionary<string,object> s,string key,string index){string value=Json.Text(s,key);if(value=="")value="未检测";int i;return Int32.TryParse(Json.Text(s,index),out i)&&i>0?value+" / if "+i:value;}
    }

    internal sealed class WhitelistForm : Surface {
        internal readonly ListBox Items;
        internal readonly TextBox Input;
        internal readonly Label Notice;
        internal readonly Button AddDomain,AddIP,Remove;
        private readonly CommandWriter commands;
        internal WhitelistForm(string root,CommandWriter writer,Dictionary<string,object> state):base("管理国外白名单（这些目标走 VPN）",new Size(620,430),root){
            commands=writer;FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=false;StartPosition=FormStartPosition.CenterParent;
            Label(this,20,16,570,28,"国外白名单（域名、IP/CIDR）",14,true);Label(this,20,48,580,24,"列表中的目标通过 VPN；国内应用仍按当前分流模式直连。",9,false,Muted);
            Items=Place(this,new ListBox{Font=UiFont(10)},20,82,580,230);
            foreach(var v in Json.Strings(state,"foreign_domains"))Items.Items.Add("域名 | "+v);foreach(var v in Json.Strings(state,"foreign_cidrs"))Items.Items.Add("IP/CIDR | "+v);
            Input=Place(this,new TextBox{Font=UiFont(10)},20,328,286,30);Cue(Input,"粘贴网址、域名或 IP/CIDR");
            AddDomain=Button(this,316,326,90,34,"添加域名",delegate{Add(false);});AddIP=Button(this,414,326,90,34,"添加 IP",delegate{Add(true);});
            Remove=Button(this,510,326,90,34,"删除选中",Delete);
            Notice=Label(this,20,374,445,42,"修改先提交，实际结果以管理台状态为准。",9,false,Muted);
            var close=Button(this,480,378,120,36,"完成",delegate{Close();},true);CancelButton=close;
        }
        internal void Add(bool ip){try{var t=Target.Parse(Input.Text);if((t.Action=="add_foreign_cidr")!=ip)throw new FormatException(ip?"请填写 IP 或 CIDR。":"请填写域名或网址。");string row=(ip?"IP/CIDR | ":"域名 | ")+t.Value;if(Items.Items.Contains(row)){Notice.Text="该目标已在白名单中。";return;}commands.Write(t.Action,null,t.Value);Items.Items.Add(row);Input.Clear();Notice.Text="操作已提交，等待程序处理…";}catch(Exception ex){Notice.Text=ex.Message;}}
        internal void Delete(){if(Items.SelectedIndex<0)return;string row=Convert.ToString(Items.SelectedItem);bool domain=row.StartsWith("域名 | ");commands.Write(domain?"remove_foreign_domain":"remove_foreign_cidr",null,row.Substring(domain?5:10));Items.Items.RemoveAt(Items.SelectedIndex);Notice.Text="删除已提交，等待程序处理…";}
    }

    internal sealed class RecommendationForm : Surface {
        private readonly Timer timer=new Timer{Interval=1000};private int count=20;
        internal RecommendationForm(string root,CommandWriter writer,Dictionary<string,object> s):base("智能选线结果",new Size(520,330),root){
            FormBorderStyle=FormBorderStyle.FixedDialog;ControlBox=false;StartPosition=FormStartPosition.CenterParent;
            Label(this,24,20,470,34,"推荐："+Json.Text(s,"smart_candidate"),16,true);
            Label(this,24,76,470,110,"ChatGPT/OpenAI："+Json.Text(s,"smart_successes")+"/"+Json.Text(s,"smart_attempts")+"\n中位耗时："+Json.Text(s,"smart_median_ms")+" ms　最慢："+Json.Text(s,"smart_slowest_ms")+" ms\n出口："+Json.Text(s,"smart_exit_ip")+" / "+Json.Text(s,"smart_exit_region"));
            var countdown=Label(this,24,198,470,28,"20 秒后自动采用推荐线路",10,true,Amber);
            Action<string> decide=delegate(string action){writer.Write(action);Close();};
            Button(this,24,248,220,42,"立即采用",delegate{decide("smart_select_accept");},true);Button(this,268,248,220,42,"恢复原线路",delegate{decide("smart_select_restore");});
            timer.Tick+=delegate{count--;countdown.Text=count+" 秒后自动采用推荐线路";if(count<=0){timer.Stop();try{decide("smart_select_accept");}catch(Exception ex){countdown.Text="提交失败，请手动选择："+ex.Message;}}};
            Shown+=delegate{timer.Start();};FormClosed+=delegate{timer.Stop();timer.Dispose();};
        }
    }
}
