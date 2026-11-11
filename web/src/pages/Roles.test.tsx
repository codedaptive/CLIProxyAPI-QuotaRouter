import {afterEach, expect, it, vi} from "vitest";
import {cleanup, fireEvent, render, screen, waitFor} from "@testing-library/react";
import {RolesPage} from "./Roles";
import type {PageProps} from "../App";
const {save} = vi.hoisted(()=>({save:vi.fn(async(_patch:unknown)=>({valid:true}))}));
vi.mock("../settingsApi",()=>({applySettings:save}));
vi.mock("../hooks",()=>({toast:vi.fn(),usePoll:(path:string)=>({data:path==="/settings"?{config:{roles:[{name:"designer",enabled:true,allow_client_effort_override:false,cooldown_seconds:30,targets:[{model:"opus",effort:"high"},{model:"sol",effort:"xhigh"}]}]}}:{decisions:[]},reload:async()=>{}})}));
afterEach(()=>{cleanup();save.mockClear()});
const props={reloadOverview:async()=>{},provider:"all",search:"",now:0} as PageProps;
it("saves target order and a client effort override together",async()=>{
 render(<RolesPage {...props}/>);
 fireEvent.click(screen.getByRole("button",{name:"Move target 2 up"}));
 fireEvent.click(screen.getByLabelText("Allow client effort override (/effort)"));
 fireEvent.click(screen.getByRole("button",{name:"Save roles"}));
 await waitFor(()=>expect(save).toHaveBeenCalledTimes(1));
 const patch=save.mock.calls[0][0] as {roles:{allow_client_effort_override:boolean;targets:{model:string;effort:string}[]}[]};
 expect(patch.roles[0].allow_client_effort_override).toBe(true);
 expect(patch.roles[0].targets.map(t=>[t.model,t.effort])).toEqual([["sol","xhigh"],["opus","high"]]);
});
it("accepts comma-separated restrictions without losing the typed comma",async()=>{
 render(<RolesPage {...props}/>);
 const input=screen.getAllByPlaceholderText("claude, antigravity")[0];
 fireEvent.change(input,{target:{value:"claude, antigravity"}});fireEvent.blur(input);
 fireEvent.click(screen.getByRole("button",{name:"Save roles"}));
 await waitFor(()=>expect(save).toHaveBeenCalledTimes(1));
 expect(JSON.stringify(save.mock.calls[0])).toContain('"providers":["claude","antigravity"]');
});
