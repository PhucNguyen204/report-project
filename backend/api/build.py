#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Regenerate the checked-in JSON OpenAPI document using Python's standard library."""
import json, pathlib, re
S={}
def ref(n): return {'$ref':'#/components/schemas/'+n}
def st(**kw): return dict(type='string',**kw)
def integer(**kw): return dict(type='integer',**kw)
def number(**kw): return dict(type='number',**kw)
def boolean(): return {'type':'boolean'}
def enum(*v): return st(enum=list(v))
def array(s,**kw): return dict(type='array',items=s,**kw)
def obj(p,required=None,**kw):
 d={'type':'object','properties':p,'additionalProperties':False};d.update(kw)
 if required is None: required=list(p)
 if required:d['required']=required
 return d
def nullable(s):return {'anyOf':[s,{'type':'null'}]}
def put(n,s):S[n]=s;return ref(n)
def flag(k):return obj({k:{'type':'boolean','const':True}})
uuid=st(format='uuid');date=st(format='date');timestamp=st(format='date-time');text=st();emptyuuid={'anyOf':[uuid,{'const':''},{'type':'null'}]};version=integer(minimum=1);money=integer(format='int64',minimum=0);dimension=enum('mass','volume','count');slot=enum('breakfast','lunch','dinner');location=enum('chill','freezer','crisper','door','pantry_shelf')
put('UUID',uuid);put('QuantityInput',{'description':'Lượng dương, tối đa hai chữ số thập phân; gửi số JSON hoặc chuỗi thập phân. Tính bằng fixed-point, không dùng số thực trong nghiệp vụ.','oneOf':[number(minimum=.01,maximum=9999999999.99,multipleOf=.01),st(pattern=r'^(?:[1-9][0-9]{0,9}(?:\.[0-9]{1,2})?|0\.(?:0[1-9]|[1-9][0-9]?))$')],'examples':[.25,'0.25']})
put('QuantityOutput',number(multipleOf=.000001,description='Giá trị fixed-point chính xác tối đa sáu chữ số thập phân; thay đổi kỳ trước có thể âm.',examples=[200,.333333]))
qi=ref('QuantityInput');qo=ref('QuantityOutput');qmap={'type':'object','propertyNames':dimension,'additionalProperties':qo}
put('FactorInput',{'oneOf':[number(exclusiveMinimum=0,multipleOf=.0001),st(pattern=r'^[0-9]+(?:\.[0-9]{1,4})?$')],'description':'Hệ số dương với tối đa bốn chữ số thập phân.'})
traceid=st(pattern=r'^[0-9a-f]{32}$',description='Mã trace OpenTelemetry để đối chiếu log; runtime trả trong X-Trace-ID.')
put('StockLevel',obj({'foodId':uuid,'lotId':uuid,'dimension':dimension,'baseQuantity':qo},description='Lượng tồn khả dụng hiện tại theo đơn vị cơ sở g/ml/cái, trong không gian của người gọi.'))
put('Error',obj({'code':integer(),'message':st(minLength=1),'traceId':traceid,'current':array(ref('StockLevel'))},['code','message'],description='Lỗi thật của backend; không có data. M06 thiếu tồn trả409/4002 kèm current sau rollback trừ kho.',examples=[{'code':4001,'message':'Không tìm thấy dữ liệu'}]))
put('Session',obj({'accessToken':text,'refreshToken':text,'expiresIn':integer(const=1800),'deviceId':uuid,'sessionId':uuid,'userId':uuid,'spaceId':uuid},['accessToken','refreshToken','expiresIn','deviceId','sessionId','userId']))
put('Password',st(minLength=8,maxLength=32,format='password',allOf=[{'pattern':r'\p{Lu}'},{'pattern':r'\p{Ll}'},{'pattern':r'\p{Nd}'}],description='8–32 ký tự, có chữ hoa, chữ thường và chữ số Unicode.'))
put('RegisterInput',obj({'fullName':st(minLength=2,maxLength=50),'email':st(format='email',maxLength=100),'password':ref('Password'),'acceptTerms':boolean()},['fullName','email','password'],examples=[{'fullName':'Nguyễn An','email':'an@example.com','password':'MatKhau123','acceptTerms':True}]))
put('OTPVerifyInput',obj({'email':st(format='email'),'purpose':enum('register','reset_password'),'code':st(pattern=r'^\d{6}$'),'deviceId':uuid,'platform':enum('android','ios')},['email','code']))
put('OTPResendInput',obj({'email':st(format='email'),'purpose':enum('register','reset_password')},['email']))
put('LoginInput',obj({'email':st(format='email'),'password':st(minLength=1,maxLength=128,format='password'),'deviceId':uuid,'platform':enum('android','ios')},['email','password']))
put('RefreshInput',obj({'refreshToken':st(minLength=1),'deviceId':uuid}))
put('Profile',obj({'id':uuid,'email':st(format='email'),'fullName':text,'locale':enum('vi','en'),'avatarObjectId':nullable(uuid),'lastSpaceId':nullable(uuid)}))
put('ProfilePatch',obj({'fullName':nullable(st(minLength=2,maxLength=50)),'locale':nullable(enum('vi','en')),'avatarObjectId':emptyuuid},[]))
put('Settings',obj({'reminderDays':nullable(integer(minimum=1,maximum=7)),'expiryNotifyOn':boolean(),'assignmentNotifyOn':boolean(),'activityNotifyOn':boolean(),'reminderRunAt':st(pattern=r'^\d{2}:\d{2}$'),'defaultReminderDays':integer(minimum=1,maximum=7)}))
put('SettingsInput',obj({'reminderDays':nullable(integer(minimum=1,maximum=7)),'expiryNotifyOn':nullable(boolean()),'assignmentNotifyOn':nullable(boolean()),'activityNotifyOn':nullable(boolean())},[]))
put('DeviceInput',obj({'deviceId':uuid,'fcmToken':st(minLength=1,maxLength=4096),'platform':enum('android','ios')},['fcmToken','platform']))
put('Notification',obj({'id':uuid,'title':text,'body':text,'type':text,'readAt':nullable(timestamp),'createdAt':timestamp}))
put('SpaceBrief',obj({'id':uuid,'kind':enum('personal','family'),'name':text,'role':enum('leader','member'),'changeSeq':integer(minimum=0),'avatarObjectId':nullable(uuid)}))
put('SpaceInput',obj({'name':st(minLength=3,maxLength=50),'copyPersonalData':obj({'lists':boolean(),'lots':boolean(),'recipes':boolean()},[])},['name'],examples=[{'name':'Gia đình An','copyPersonalData':{'lots':True,'recipes':True}}]))
put('Member',obj({'userId':uuid,'role':enum('leader','member'),'fullName':text,'avatarObjectId':nullable(uuid)}))
put('Activity',obj({'seq':integer(),'kind':text,'entityId':nullable(text),'actorId':nullable(uuid),'actorName':nullable(text),'payload':{},'createdAt':timestamp}))
put('SpaceDetail',obj({'id':uuid,'name':text,'kind':enum('personal','family'),'changeSeq':integer(minimum=0),'members':array(ref('Member')),'recentActivity':array(ref('Activity'),maxItems=20)}))
put('SpacePatch',obj({'name':nullable(st(minLength=3,maxLength=50)),'avatarObjectId':emptyuuid},[]))
put('JoinInput',obj({'code':st(minLength=1),'dryRun':boolean()},['code']))
put('InviteState',obj({'status':enum('none','active','expired'),'code':nullable(st(pattern=r'^[A-HJ-NP-Z2-9]{8}$')),'expiresAt':nullable(timestamp),'link':nullable(st(format='uri')),'memberCount':integer(minimum=1),'maxMembers':integer(minimum=2,maximum=30)}))
put('JoinResult',obj({'id':uuid,'name':text,'memberCount':integer(minimum=1),'dryRun':boolean(),'leaderName':text,'maxMembers':integer(minimum=2,maximum=30),'role':enum('','leader','member'),'joinedAt':nullable(timestamp)}))
put('Category',obj({'id':uuid,'name':text}))
put('CatalogUnit',obj({'id':uuid,'name':text,'dimension':dimension,'factorToBase':number(exclusiveMinimum=0),'isBase':boolean()}))
put('Catalog',obj({'categories':array(ref('Category')),'units':array(ref('CatalogUnit')),'locations':array(location)}))
put('NewFood',obj({'name':st(minLength=1,maxLength=100),'categoryId':uuid}))
put('LotInput',obj({'id':uuid,'foodId':uuid,'unitId':uuid,'quantity':qi,'storageLocation':location,'purchasedOn':date,'expiresOn':date,'notes':st(maxLength=200),'confirmExpired':boolean(),'newFood':nullable(ref('NewFood'))},['unitId','quantity','storageLocation'],anyOf=[{'required':['foodId']},{'required':['newFood']}]))
put('Lot',obj({'id':uuid,'foodId':uuid,'name':text,'unitId':uuid,'dimension':dimension,'initialQuantity':qo,'remainingQuantity':qo,'storageLocation':location,'purchasedOn':date,'expiresOn':date,'expiryState':enum('','fresh','expiring','expired'),'lifecycle':enum('active','consumed','discarded'),'notes':text,'version':version,'totalCost':money},['id','foodId','name','unitId','dimension','initialQuantity','remainingQuantity','storageLocation','purchasedOn','expiresOn','expiryState','lifecycle','notes','version']))
put('Movement',obj({'id':uuid,'kind':enum('stock_in','consume','discard','adjust'),'quantity':qo,'unitId':uuid,'reason':text,'mealId':uuid,'occurredAt':timestamp},['id','kind','quantity','unitId','reason','occurredAt']))
put('LotDetail',{'allOf':[ref('Lot'),obj({'movements':array(ref('Movement'))},additionalProperties=True)]})
# LotDetail's base object must accept the added movements property.
S['LotDetail']=obj(dict(S['Lot']['properties'],movements=array(ref('Movement'))),list(S['Lot']['required'])+['movements'])
put('LotPatch',obj({'remainingQuantity':nullable({'oneOf':[number(minimum=0,multipleOf=.01),st(pattern=r'^[0-9]+(?:\.[0-9]{1,2})?$')]}),'storageLocation':nullable(location),'expiresOn':nullable(date),'notes':nullable(st(maxLength=200))},[]))
put('ListInput',obj({'id':uuid,'name':st(minLength=1,maxLength=100),'plannedDate':date,'notes':st(maxLength=200)},['name']))
put('ShoppingList',obj({'id':uuid,'name':text,'plannedDate':date,'status':enum('draft','shopping','completed'),'notes':text,'version':version,'totalCost':money}))
put('ShoppingListSummary',obj(dict(S['ShoppingList']['properties'],itemCount=integer(minimum=0),purchasedBy=array(uuid))))
put('ShoppingMonth',obj({'month':st(pattern=r'^\d{4}-\d{2}$'),'totalCost':money,'listCount':integer(minimum=0)}))
put('ListPatch',obj({'name':nullable(st(minLength=1,maxLength=100)),'plannedDate':nullable(date),'notes':nullable(st(maxLength=200))},[]))
put('ItemInput',obj({'id':uuid,'foodId':uuid,'name':st(minLength=1,maxLength=100),'unitId':uuid,'quantity':qi,'notes':st(maxLength=200)},['unitId','quantity'],anyOf=[{'required':['foodId']},{'required':['name']}]))
put('ShoppingItem',obj({'id':uuid,'listId':uuid,'foodId':nullable(uuid),'name':text,'unitId':uuid,'dimension':dimension,'quantity':qo,'actualQuantity':nullable(qo),'unitPrice':nullable(money),'status':enum('to_buy','assigned','purchased','stocked','skipped'),'assigneeId':nullable(uuid),'purchasedBy':nullable(uuid),'purchasedAt':nullable(timestamp),'lotId':nullable(uuid),'notes':text,'version':version}))
put('ItemPatch',obj({'name':nullable(st(minLength=1,maxLength=100)),'quantity':nullable(qi),'actualQuantity':nullable(qi),'unitPrice':nullable(money),'notes':nullable(st(maxLength=200)),'status':nullable(enum('skipped'))},[]))
put('PurchaseLine',obj({'itemId':uuid,'actualQuantity':nullable(qi),'unitPrice':nullable(money),'onBehalf':boolean(),'purchasedAt':nullable(timestamp)},['itemId']))
put('PurchaseResult',obj({'itemId':uuid,'outcome':enum('purchased','already_purchased'),'current':ref('ShoppingItem')}))
put('StockLine',obj({'itemId':uuid,'foodId':uuid,'storageLocation':location,'quantity':nullable(qi),'unitId':uuid,'purchasedOn':date,'expiresOn':date},['itemId','storageLocation']))
put('StockResult',obj({'itemId':uuid,'outcome':enum('stocked','already_stocked'),'lotId':uuid,'expiresOn':date,'expiryState':enum('fresh','expiring','expired')}))
put('IngredientInput',obj({'foodId':uuid,'unitId':uuid,'quantity':qi,'optional':boolean()},['foodId','unitId','quantity']))
put('IngredientOutput',obj({'foodId':uuid,'unitId':uuid,'quantity':qo,'optional':boolean()}))
recipe_fields={'id':uuid,'name':st(minLength=2,maxLength=100),'servings':integer(minimum=1,maximum=20),'mealSlot':enum('','breakfast','lunch','dinner'),'cookMinutes':integer(minimum=0,maximum=1440),'steps':nullable(array(text)),'notes':text,'imageObjectId':emptyuuid,'ingredients':array(ref('IngredientInput'),minItems=1,maxItems=100)}
put('RecipeInput',obj(recipe_fields,['name','servings','ingredients'],examples=[{'name':'Canh cà rốt','servings':2,'ingredients':[{'foodId':'00000000-0000-7000-8000-000000000021','unitId':'00000000-0000-7000-8000-000000000012','quantity':'0.2'}]}]))
put('Comparison',obj({'foodId':uuid,'unitId':uuid,'required':qo,'available':qo,'shortQty':qo,'optional':boolean(),'dimension':dimension}))
put('Recipe',obj(dict(recipe_fields,ingredients=array(ref('IngredientOutput')),kind=enum('space','template'),status=enum('draft','published','hidden'),version=version,comparison=array(ref('Comparison'))),['id','name','servings','mealSlot','cookMinutes','steps','notes','ingredients','kind','status','version']))
put('TemplateInput',obj(dict(recipe_fields,status=enum('draft','published','hidden')),['name','servings','ingredients']))
meal_fields={'id':uuid,'recipeId':uuid,'name':st(minLength=2,maxLength=100),'date':date,'mealSlot':slot,'servings':integer(minimum=1,maximum=20)}
put('MealInput',obj(meal_fields,['date','mealSlot','servings'],anyOf=[{'required':['recipeId']},{'required':['name']}]))
put('MealPatch',obj({k:nullable(v) for k,v in meal_fields.items() if k!='id'},[]))
put('Meal',obj(dict(meal_fields,recipeId=nullable(uuid),status=enum('planned','cooked'),version=version,cookedAt=nullable(timestamp),cookedBy=nullable(uuid),skippedStock=boolean())))
nonnegative_quantity={'oneOf':[number(minimum=0,maximum=9999999999.99,multipleOf=.01),st(pattern=r'^(?:0|[1-9][0-9]{0,9})(?:\.[0-9]{1,2})?$')],'description':'0 là nguyên liệu lấy ngoài tủ, không tạo biến động; lượng dương có tối đa2 chữ số thập phân.'}
put('CookLine',obj({'foodId':uuid,'unitId':uuid,'quantity':nonnegative_quantity,'lotId':uuid,'lotIds':array(uuid,maxItems=100,uniqueItems=True),'allowExpired':boolean()},['foodId','unitId','quantity']))
S['CookLine']['allOf']=[{'not':{'allOf':[{'required':['lotId']},{'required':['lotIds'],'properties':{'lotIds':{'minItems':1}}}]}},{'if':{'properties':{'quantity':{'anyOf':[{'const':0},st(pattern=r'^0(?:\.0{1,2})?$')]}},'required':['quantity']},'then':{'not':{'anyOf':[{'required':['lotId']},{'required':['lotIds'],'properties':{'lotIds':{'minItems':1}}}]}}}]
put('StockDeduction',obj({'foodId':uuid,'lotId':uuid,'unitId':uuid,'dimension':dimension,'quantity':qo},description='Lượng đã trừ theo đơn vị cơ sở g/ml/cái; unitId là đơn vị cơ sở của dimension.'))
put('CookResult',obj(dict(S['Meal']['properties'],meal=ref('Meal'),deductions=array(ref('StockDeduction')),alreadyCooked=boolean()),description='Giữ trường Meal dạng phẳng để tương thích; meal/deductions/actor/time luôn lấy dữ liệu đã lưu khi replay.'))
put('CookInput',obj({'servings':integer(minimum=1,maximum=20),'skipStock':boolean(),'lines':nullable(array(ref('CookLine'),maxItems=100))},[]))
put('ConfirmLine',obj({'foodId':uuid,'unitId':uuid,'quantity':qi}))
missing_fields={'dryRun':boolean(),'targetListId':uuid,'plannedDate':date,'name':st(maxLength=100),'lines':array(ref('ConfirmLine'),minItems=1,maxItems=100)}
put('GenerateInput',obj(dict(missing_fields,**{'from':date,'to':date}),['from','to'],description='dryRun=true chỉ xem trước, không cần Idempotency-Key. Xác nhận dùng lines do người dùng giữ/chỉnh và chọn targetListId hoặc plannedDate.'))
put('RecipeMissingInput',obj(dict(missing_fields,servings=integer(minimum=1,maximum=20)),['servings']))
put('MissingMeal',obj({'mealId':uuid,'mealDate':date,'name':text}))
put('MissingPreviewLine',obj({'foodId':uuid,'name':text,'shortQty':qo,'unitId':uuid,'neededBy':date,'meals':array(ref('MissingMeal'))}))
put('MissingResult',obj({'lines':array(ref('MissingPreviewLine')),'skippedMeals':array(ref('MissingMeal')),'suggestedPlannedDate':date,'listId':uuid},[]))
put('SuggestedFood',obj({'foodId':uuid,'name':text,'daysLeft':integer()},['foodId','name']))
put('SuggestedRecipe',obj({'recipe':ref('Recipe'),'score':number(),'r':number(),'e':number(),'f':number(),'missing':array(ref('SuggestedFood')),'expiringUsed':array(ref('SuggestedFood'))}))
put('Suggestions',obj({'items':array(ref('SuggestedRecipe')),'meta':obj({'fallback':enum('none','empty_pantry','below_threshold'),'theta':number()})}))
put('AIRecommendationInput',obj({'date':date,'mealSlot':slot,'servings':integer(minimum=1,maximum=20),'limit':integer(minimum=1,maximum=10),'expiringOnly':boolean()},['servings','limit']))
put('AIRecommendationItem',obj({'recipe':ref('Recipe'),'reason':st(minLength=1,maxLength=500)}))
put('AIRecommendationJob',obj({'jobId':uuid,'status':enum('pending','ready','failed'),'expiresAt':timestamp,'items':array(ref('AIRecommendationItem'),maxItems=10),'errorCode':enum('provider_unavailable','provider_rejected','invalid_response','access_revoked','expired','invalid_request')},['jobId','status','expiresAt','items']))
put('ReportSummary',obj({'from':date,'to':date,'totalCost':money,'quantities':qmap,'lots':integer(),'unpricedLots':integer(),'cookedMeals':integer(),'wasteRate':number()}))
put('ReportChange',obj({'totalCost':integer(format='int64'),'quantities':qmap,'lots':integer(),'unpricedLots':integer(),'cookedMeals':integer(),'wasteRate':number()}))
put('ReportPoint',obj({'period':date,'totalCost':money,'quantities':qmap}))
put('ReportCategory',obj({'categoryId':st(),'name':text,'totalCost':money,'quantities':qmap}))
put('ReportFood',obj({'foodId':st(),'name':text,'dimension':dimension,'quantity':qo,'totalCost':money}))
put('ReportReason',obj({'reason':text,'lots':integer(),'totalCost':money}))
put('Report',obj({'previous':ref('ReportSummary'),'change':ref('ReportChange'),'categories':nullable(array(ref('ReportCategory'))),'type':enum('spending','consumption','waste'),'from':date,'to':date,'groupBy':enum('day','week','month'),'totalCost':money,'quantities':qmap,'unpricedLots':integer(),'lots':integer(),'cookedMeals':integer(),'wasteRate':number(),'empty':boolean(),'series':array(ref('ReportPoint')),'topFoods':array(ref('ReportFood')),'reasons':array(ref('ReportReason'))}))
put('ExportInput',obj({'reportType':enum('spending','consumption','waste'),'from':date,'to':date,'format':enum('pdf','csv'),'groupBy':enum('day','week','month')},['reportType','from','to','format']))
put('Export',obj({'exportId':uuid,'status':enum('pending','ready','failed'),'downloadUrl':st(format='uri'),'error':text,'expiresAt':timestamp},['exportId','status','expiresAt'],allOf=[{'if':{'properties':{'status':{'const':'ready'}}},'then':{'required':['downloadUrl']}},{'if':{'properties':{'status':{'const':'failed'}}},'then':{'required':['error']}}]))
put('MediaInput',obj({'purpose':enum('avatar','space','recipe','food'),'targetId':uuid,'contentType':enum('image/png','image/jpeg'),'size':integer(minimum=1,maximum=5242880)}))
put('MediaGrant',obj({'objectId':uuid,'uploadUrl':st(format='uri'),'headers':{'type':'object','additionalProperties':text},'expiresAt':timestamp}))
put('Download',obj({'downloadUrl':st(format='uri'),'expiresAt':timestamp}))
put('SearchFood',obj({'id':uuid,'name':text,'type':{'const':'food'},'categoryId':nullable(uuid),'unitId':uuid,'defaultLocation':location,'shelfLives':{'type':'object','additionalProperties':integer(minimum=1)}}))
put('SearchRecipe',obj({'id':uuid,'name':text,'type':{'const':'recipe'}}))
put('SearchLot',obj({'id':uuid,'name':text,'foodId':uuid,'type':{'const':'lot'},'quantity':st(pattern=r'^[0-9]+(?:\.[0-9]{1,6})?$'),'unitId':uuid,'location':location,'expiresOn':date}))
put('Search',obj({'items':array({'oneOf':[ref('SearchFood'),ref('SearchRecipe'),ref('SearchLot')]}),'facets':{'type':'object','additionalProperties':integer(minimum=0)}}))
put('SearchPage',obj(dict(S['Search']['properties'],filterFacets=obj({k:{'type':'object','additionalProperties':integer(minimum=0)} for k in ['categoryId','location','expiryState']}))))
put('CategoryInput',obj({'id':uuid,'name':st(minLength=1,maxLength=100),'active':nullable(boolean())},['name']))
put('UnitInput',obj({'id':uuid,'name':st(minLength=1,maxLength=100),'dimension':dimension,'factorToBase':ref('FactorInput'),'isBase':boolean(),'active':nullable(boolean())},['name','dimension','factorToBase']))
put('FoodInput',obj({'id':uuid,'name':st(minLength=1,maxLength=100),'categoryId':uuid,'defaultUnitId':uuid,'status':enum('active','inactive'),'defaultLocation':location,'shelfLives':{'type':'object','additionalProperties':integer(minimum=1,maximum=3650)}},['name','categoryId','defaultUnitId']))
put('AdminCategory',obj({'id':uuid,'name':text,'active':boolean()}));put('AdminUnit',obj({'id':uuid,'name':text,'dimension':dimension,'factor_to_base':number(),'is_base':boolean(),'active':boolean()}))
put('AdminFood',obj({'id':uuid,'name':text,'category_id':uuid,'scope':enum('global','space'),'owner_space_id':nullable(uuid),'default_unit_id':uuid,'status':enum('active','inactive','pending_review'),'default_location':text,'merged_into':nullable(uuid),'version':version}))
put('AdminUser',obj({'id':uuid,'email':st(format='email'),'fullName':text,'systemRole':enum('user','admin'),'status':enum('pending','active','disabled'),'lastLoginAt':nullable(timestamp),'lockReason':nullable(text)}))
put('AdminUserPatch',obj({'status':enum('active','disabled'),'systemRole':enum('user','admin'),'lockReason':text},[]))
config_fields={k:integer(minimum=a,maximum=b) for k,(a,b) in {'reminder.defaultDays':(1,7),'reminder.maxLotsPerNotice':(1,10),'group.maxMembers':(2,30),'invite.ttlHours':(1,168),'otp.ttlMinutes':(2,15),'suggest.maxResults':(3,30),'mealplan.horizonDays':(7,60)}.items()}
config_fields.update({'reminder.runAt':st(pattern=r'^(?:0[5-9]|1[0-9]|20):[0-5][0-9]$|^21:00$'),'suggest.theta':number(minimum=.3,maximum=1),'suggest.weights':obj({'r':number(minimum=0,maximum=1),'e':number(minimum=0,maximum=1),'f':number(minimum=0,maximum=1)})})
put('ConfigInput',obj({'values':obj(config_fields,[],minProperties=1)}));put('Config',obj({'key':enum(*config_fields),'value':{'anyOf':[integer(),number(),text,ref('ConfigInput') if False else config_fields['suggest.weights']]}}))
put('Audit',obj({'id':uuid,'actorId':uuid,'action':text,'entityId':nullable(text),'oldValue':{},'newValue':{},'createdAt':timestamp}))
for name in ['Profile','SpaceBrief','SpaceDetail','AdminUser']:
 S[name]['properties']['version']=version
 S[name]['required'].append('version')
for output,source in [('LotPatchResult','Lot'),('ListPatchResult','ShoppingList'),('ItemPatchResult','ShoppingItem')]:
 S[output]=obj(dict(S[source]['properties'],overwrittenFields=array(text)),list(S[source]['required'])+['overwrittenFields'])
S['MissingResult']['anyOf']=[{'required':['suggestedPlannedDate']},{'required':['listId']}]
for name in ['GenerateInput','RecipeMissingInput']:
 S[name]['allOf']=[{'if':{'properties':{'dryRun':{'const':True}},'required':['dryRun']},'else':{'required':['lines'],'oneOf':[{'required':['targetListId'],'not':{'required':['plannedDate']}},{'required':['plannedDate'],'not':{'required':['targetListId']}}]}}]
P={};prefix='/it4788/v1';base=prefix+'/spaces/{sid}'
params={
 'AppVersion':{'name':'X-App-Version','in':'header','required':True,'schema':st(pattern=r'^\d+\.\d+\.\d+$'),'example':'1.0.0','description':'Phiên bản ứng dụng x.y.z; thấp hơn mức tối thiểu nhận HTTP426.'},
 'IdempotencyKey':{'name':'Idempotency-Key','in':'header','required':True,'schema':st(format='uuid'),'example':'01a1d000-0000-7000-8000-000000000001','description':'Giữ nguyên khóa khi thử lại cùng method, URL và body; đổi nội dung cùng khóa nhận400.'},
 'PreviewIdempotencyKey':{'name':'Idempotency-Key','in':'header','required':False,'schema':st(format='uuid'),'description':'Bắt buộc khi xác nhận; dryRun=true không cần khóa.','x-required-when':'body.dryRun != true'},
 'IfMatch':{'name':'If-Match','in':'header','required':True,'schema':st(pattern=r'^(?:[1-9][0-9]*|"[1-9][0-9]*")$'),'example':'"1"','description':'Phiên bản người dùng đã đọc. Với M06, món đã cooked được trả lại dù phiên bản cũ; không trừ kho lần nữa.'},
 'IfNoneMatch':{'name':'If-None-Match','in':'header','required':False,'schema':text,'description':'ETag đã lưu; khi không thay đổi trả304 không có body.'}}
def par(name,schema,default=None,required=False,description=''):
 if default is not None:schema=dict(schema,default=default)
 return {'name':name,'in':'query','required':required,'schema':schema,'description':description}
def envelope(data,code=1000):return obj({'code':integer(const=code),'data':data})
def op(code,method,path,summary,response,request=None,status=200,uc=(),public=False,queries=(),match=False,preview=False,extra_status=None,description=''):
 path=path if path.startswith('/') else prefix+path;method=method.lower();h=[]
 if path not in ['/healthz','/readyz','/openapi.json']:h.append({'$ref':'#/components/parameters/AppVersion'})
 if method not in ['get','head','options'] and not public and code!='A11':h.append({'$ref':'#/components/parameters/'+('PreviewIdempotencyKey' if preview else 'IdempotencyKey')})
 if match:h.append({'$ref':'#/components/parameters/IfMatch'})
 for name in re.findall(r'\{([^}]+)\}',path):h.append({'name':name,'in':'path','required':True,'schema':uuid})
 h.extend(queries)
 name=code+'_'+method;data_schema=put(name+'Response',envelope(response,1001 if status==202 else 1000))
 responses={str(status):{'description':'Thành công','content':{'application/json':{'schema':data_schema}}}}
 for extra,sch in (extra_status or {}).items():responses[str(extra)]={'description':'Thành công','content':{'application/json':{'schema':put(name+'Response'+str(extra),envelope(sch))}}}
 for error in [400,401,403,404,409,413,415,422,423,426,429,500,503]:responses[str(error)]={'$ref':'#/components/responses/Error'}
 for response in responses.values():
  if '$ref' not in response:response.setdefault('headers',{}).update({'X-Request-ID':{'schema':uuid},'X-Trace-ID':{'schema':traceid}})
 d={'operationId':name,'summary':summary,'tags':['AI' if code.startswith('AI') else code[:1] if len(code)==3 else 'Vận hành'],'x-api-code':code,'x-use-case':list(uc),'parameters':h,'responses':responses}
 if public:d['security']=[]
 if description:d['description']=description
 if request is not None:d['requestBody']={'required':True,'content':{'application/json':{'schema':request}}}
 P.setdefault(path,{})[method]=d
 return d
# Identity and personal settings.
op('A01','POST',prefix+'/auth/register','Đăng ký và gửi OTP',obj({'userId':uuid,'status':enum('pending')}),ref('RegisterInput'),201,['UC01'],True,extra_status={200:obj({'userId':uuid,'status':enum('pending')})})
op('A02','POST',prefix+'/auth/otp/verify','Xác thực OTP',{'oneOf':[ref('Session'),obj({'resetToken':text})]},ref('OTPVerifyInput'),uc=['UC01','UC03'],public=True)
op('A03','POST',prefix+'/auth/otp/resend','Gửi lại OTP',flag('accepted'),ref('OTPResendInput'),uc=['UC01'],public=True)
op('A04','POST',prefix+'/auth/login','Đăng nhập và cấp phiên',ref('Session'),ref('LoginInput'),uc=['UC02'],public=True)
op('A05','POST',prefix+'/auth/refresh','Làm mới phiên',ref('Session'),ref('RefreshInput'),public=True)
op('A06','POST',prefix+'/auth/logout','Đăng xuất và thu hồi phiên',flag('loggedOut'),uc=['UC02'])
op('A07','POST',prefix+'/auth/password/forgot','Yêu cầu OTP khôi phục mật khẩu',flag('accepted'),obj({'email':st(format='email')}),uc=['UC03'],public=True)
op('A08','POST',prefix+'/auth/password/reset','Đặt lại mật khẩu',flag('reset'),obj({'email':st(format='email'),'resetToken':text,'newPassword':ref('Password')}),uc=['UC03'],public=True)
op('A09','GET',prefix+'/me','Xem hồ sơ',ref('Profile'),uc=['UC04'])
op('A10','PATCH',prefix+'/me','Cập nhật hồ sơ',obj({'updated':{'const':True},'version':version,'overwrittenFields':array(text)}),ref('ProfilePatch'),uc=['UC04'],match=True)
op('A11','PUT',prefix+'/me/password','Đổi mật khẩu và cấp phiên hiện tại mới',ref('Session'),obj({'currentPassword':text,'newPassword':ref('Password')}),uc=['UC03'])
op('A12','GET',prefix+'/me/settings','Xem thiết lập nhắc nhở',ref('Settings'),uc=['UC04'])
op('A13','PUT',prefix+'/me/settings','Lưu thiết lập nhắc nhở',flag('updated'),ref('SettingsInput'),uc=['UC04'])
op('A14','POST',prefix+'/me/devices','Đăng ký thiết bị nhận thông báo',obj({'deviceId':uuid}),ref('DeviceInput'),uc=['UC14'])
op('A15','DELETE',prefix+'/me/devices/{deviceId}','Hủy đăng ký thiết bị',flag('deleted'),uc=['UC02'])
op('A16','DELETE',prefix+'/me','Xóa tài khoản và lên lịch ẩn danh',flag('deleted'),obj({'password':text}))
op('A17','GET',prefix+'/me/notifications','Hộp thư thông báo của người gọi',obj({'items':array(ref('Notification'))}),uc=['UC14'],queries=[par('limit',integer(minimum=1,maximum=100),20),par('cursor',st(maxLength=512))])
op('A18','POST',prefix+'/me/notifications/read','Đánh dấu thông báo đã đọc',flag('updated'),obj({'ids':array(uuid,minItems=1,maxItems=100)}),uc=['UC14'])
# Households.
op('G01','GET',prefix+'/spaces','Liệt kê không gian của người dùng',obj({'items':array(ref('SpaceBrief'))}))
op('G02','POST',prefix+'/spaces','Tạo nhóm gia đình',obj({'id':uuid,'kind':enum('family'),'name':text}),ref('SpaceInput'),201,['UC05'])
op('G03','GET',base,'Xem nhóm và hoạt động gần đây',ref('SpaceDetail'),uc=['UC05'])
op('G04','PATCH',base,'Đổi tên hoặc ảnh nhóm',obj({'id':uuid,'version':version,'overwrittenFields':array(text)}),ref('SpacePatch'),uc=['UC05'],match=True)
op('G05','POST',base+'/invites','Tạo mã mời mới',obj({'code':text,'expiresAt':timestamp,'link':st(format='uri')}),status=201,uc=['UC06'])
op('G05','GET',base+'/invites/current','Xem mã mời hiện tại và trạng thái hết hạn',ref('InviteState'),uc=['UC06'],description='Chỉ trưởng nhóm. Không tạo hoặc rotate khi đọc; code/expiresAt/link null khi không có mã chưa thu hồi. Nhóm đầy trả422 trước hiển thị mã; thu hồi vẫn có thể thực hiện.')
op('G06','DELETE',base+'/invites/current','Thu hồi mã mời hiện tại',flag('revoked'),uc=['UC06'])
op('G07','POST',prefix+'/spaces/join','Xem trước hoặc tham gia nhóm',ref('JoinResult'),ref('JoinInput'),uc=['UC06'],preview=True)
op('G08','DELETE',base+'/members/{userId}','Xóa thành viên',flag('removed'),uc=['UC07'])
op('G09','POST',base+'/leader-transfer','Chuyển quyền trưởng nhóm',obj({'leaderId':uuid}),obj({'userId':uuid}),uc=['UC07'])
op('G10','POST',base+'/leave','Rời hoặc giải tán nhóm khi là người cuối',flag('left'),uc=['UC07'])
# Pantry and shopping.
page_queries=[par('limit',integer(minimum=1,maximum=100),20),par('cursor',st(maxLength=4096),description='Giữ nguyên đường dẫn, người dùng và bộ lọc của trang trước; có thể đổi limit.')]
filters=[par('limit',integer(minimum=1,maximum=100),20),par('cursor',st(maxLength=4096),description='Giữ nguyên đường dẫn, người dùng và bộ lọc của trang trước; có thể đổi limit.'),par('q',st()),*[par(k,array(st()),description='Nhiều giá trị ngăn bằng dấu phẩy hoặc lặp tham số; OR trong trường, AND giữa các trường.') for k in ['location','storageLocation','expiryState','categoryId']]]
lot_list=obj({'lots':array(ref('Lot')),'facets':obj({k:{'type':'object','additionalProperties':integer(minimum=0)} for k in ['location','expiryState','categoryId']}),'nextCursor':text})
op('P01','GET',base+'/lots','Xem và lọc lô thực phẩm',lot_list,uc=['UC12','UC20'],queries=filters)
op('P02','POST',base+'/lots','Nhập thực phẩm thủ công',ref('Lot'),ref('LotInput'),201,['UC11'])
op('P03','GET',base+'/lots/{lotId}','Chi tiết lô và biến động',ref('LotDetail'),uc=['UC12'],queries=page_queries,description='movements phân trang theo occurredAt ASC/id ASC; metadata thuộc lịch sử, data vẫn gồm chi tiết lô hiện tại và ETag.')
p04=op('P04','PATCH',base+'/lots/{lotId}','Điều chỉnh lô thực phẩm',ref('LotPatchResult'),ref('LotPatch'),uc=['UC13'],match=True,description='Thay lượng kiểm phiên bản nghiêm ngặt. Các trường mô tả dùng phiên bản nền và báo overwrittenFields khi ghi đè trường đã đổi.')

op('P05','POST',base+'/lots/{lotId}/consume','Sử dụng thực phẩm theo FEFO hoặc chọn lô',obj({'foodId':uuid}),obj({'quantity':qi,'unitId':uuid,'strategy':enum('fefo','lot')},['quantity','unitId']),uc=['UC13'])
op('P06','POST',base+'/lots/{lotId}/discard','Loại bỏ thực phẩm',obj({'remainingQuantity':qo}),obj({'quantity':qi,'reason':st(minLength=1,maxLength=200)}),uc=['UC13'])
op('P07','GET',base+'/lots/expiring','Xem lô sắp hoặc đã hết hạn',lot_list,uc=['UC12'],queries=filters)
op('S01','GET',base+'/shopping-lists','Xem danh sách và lịch sử mua sắm',obj({'lists':array(ref('ShoppingListSummary')),'months':array(ref('ShoppingMonth'))},['lists']),uc=['UC08','UC10'],queries=[par('limit',integer(minimum=1,maximum=100),20),par('cursor',st(maxLength=4096),description='Giữ nguyên đường dẫn, người dùng và bộ lọc của trang trước; có thể đổi limit.'),par('status',array(enum('draft','shopping','completed'))),par('from',date),par('to',date),par('groupBy',enum('month'))])
op('S02','POST',base+'/shopping-lists','Tạo danh sách mua sắm',ref('ShoppingList'),ref('ListInput'),201,['UC08'])
op('S03','GET',base+'/shopping-lists/{listId}','Chi tiết danh sách mua sắm',obj({'list':ref('ShoppingList'),'items':array(ref('ShoppingItem'))}),uc=['UC08'])
op('S04','PATCH',base+'/shopping-lists/{listId}','Sửa danh sách mua sắm',ref('ListPatchResult'),ref('ListPatch'),uc=['UC08'],match=True)
op('S05','DELETE',base+'/shopping-lists/{listId}','Xóa danh sách mua sắm',flag('deleted'),uc=['UC08'])
op('S06','POST',base+'/shopping-lists/{listId}/copy','Sao chép danh sách, bỏ trạng thái mua cũ',obj({'id':uuid}),obj({'id':uuid,'name':st(minLength=1,maxLength=100),'plannedDate':date,'notes':st(maxLength=200)},[]),201,['UC08'])
op('S07','POST',base+'/shopping-lists/{listId}/items','Thêm hoặc gộp mặt hàng',ref('ShoppingItem'),ref('ItemInput'),201,['UC08'])
op('S08','PATCH',base+'/shopping-items/{itemId}','Sửa mặt hàng',ref('ItemPatchResult'),ref('ItemPatch'),uc=['UC08'],match=True)
op('S09','DELETE',base+'/shopping-items/{itemId}','Xóa mặt hàng',flag('deleted'),uc=['UC08'])
op('S10','PUT',base+'/shopping-items/{itemId}/assignee','Phân công người mua',ref('ShoppingItem'),obj({'userId':uuid}),uc=['UC08'],match=True)
op('S11','DELETE',base+'/shopping-items/{itemId}/assignee','Bỏ phân công người mua',ref('ShoppingItem'),uc=['UC08'],match=True)
op('S12','POST',base+'/shopping-items/purchase','Đánh dấu các mặt hàng đã mua',obj({'results':array(ref('PurchaseResult'))}),obj({'items':array(ref('PurchaseLine'),minItems=1,maxItems=50)}),uc=['UC09'])
op('S13','POST',base+'/shopping-items/{itemId}/undo-purchase','Hoàn tác đã mua',flag('undone'),uc=['UC09'])
op('S14','POST',base+'/shopping-items/stock','Nhập mặt hàng đã mua vào tủ',obj({'results':array(ref('StockResult'))}),obj({'items':array(ref('StockLine'),minItems=1,maxItems=50)}),uc=['UC11'])
op('S15','POST',base+'/shopping-lists/generate','Sinh danh sách từ lịch bữa ăn',ref('MissingResult'),ref('GenerateInput'),201,['UC10'],preview=True,extra_status={200:ref('MissingResult')})
# Recipes, meals, suggestions and search.
op('R01','GET',base+'/recipes','Bộ sưu tập công thức',obj({'items':array(ref('Recipe'))}),uc=['UC16','UC17'],queries=page_queries+[par('q',text),par('source',enum('space','template','favorite'))])
op('R02','POST',base+'/recipes','Thêm công thức',ref('Recipe'),ref('RecipeInput'),201,['UC15'])
op('R03','GET',base+'/recipes/{recipeId}','Chi tiết và đối chiếu nguyên liệu',ref('Recipe'),uc=['UC16'],queries=[par('servings',integer(minimum=1,maximum=20))])
op('R04','PUT',base+'/recipes/{recipeId}','Cập nhật công thức',ref('Recipe'),ref('RecipeInput'),uc=['UC15'],match=True)
op('R05','DELETE',base+'/recipes/{recipeId}','Xóa công thức, giữ tên món đã lập lịch',flag('deleted'),uc=['UC15'])
op('R06','PUT',prefix+'/me/favorite-recipes/{recipeId}','Yêu thích công thức',obj({'favorite':{'const':True}}),uc=['UC15'])
op('R07','DELETE',prefix+'/me/favorite-recipes/{recipeId}','Bỏ yêu thích công thức',obj({'favorite':{'const':False}}),uc=['UC15'])
op('R08','POST',base+'/recipes/{recipeId}/missing-to-list','Bổ sung phần thiếu từ công thức',ref('MissingResult'),ref('RecipeMissingInput'),201,['UC10'],preview=True,extra_status={200:ref('MissingResult')})
op('R09','POST',base+'/recipes/{recipeId}/copy','Sao chép công thức mẫu',ref('Recipe'),obj({'name':st(minLength=2,maxLength=100)},[]),201,['UC15','UC23'])
op('M01','GET',base+'/meals','Xem lịch bữa ăn',obj({'items':array(ref('Meal'))}),uc=['UC17'],queries=page_queries+[par('from',date),par('to',date)])
op('M02','POST',base+'/meals','Thêm món vào lịch',ref('Meal'),ref('MealInput'),201,['UC17'])
op('M03','PATCH',base+'/meals/{mealId}','Sửa món chưa nấu',ref('Meal'),ref('MealPatch'),uc=['UC17'],match=True)
op('M04','DELETE',base+'/meals/{mealId}','Xóa món chưa nấu',flag('deleted'),uc=['UC17'])
op('M05','GET',base+'/recipe-suggestions','Gợi ý món từ thực phẩm sẵn có',ref('Suggestions'),uc=['UC12','UC18'],queries=[par('date',date),par('mealSlot',slot),par('servings',integer(minimum=1,maximum=20)),par('limit',integer(minimum=1,maximum=30)),par('expiringOnly',boolean(),False)])
op('AI01','POST',base+'/ai-recommendations','Yêu cầu AI xếp hạng công thức hợp lệ',ref('AIRecommendationJob'),ref('AIRecommendationInput'),202,['UC18'],description='Opt-in gửi tên món và nguyên liệu tối thiểu tới OpenAI. Tối đa10 job/người/ngày UTC; replay không tăng quota. Job hết hạn24 giờ, provider chưa cấu hình trả503 sau kiểm quyền.')
op('AI02','GET',base+'/ai-recommendations/{jobId}','Đọc gợi ý AI thuộc người gọi',ref('AIRecommendationJob'),uc=['UC18'],description='Chỉ chủ job đang là thành viên được đọc. Công thức không còn hiển thị được loại khỏi items. Không thay đổi tồn kho hoặc lịch bữa ăn.')
op('M06','POST',base+'/meals/{mealId}/cook','Xác nhận đã nấu và trừ kho nguyên tử',ref('CookResult'),ref('CookInput'),uc=['UC19'],match=True,description='CookLine0 lấy ngoài tủ; lotIds chọn tối đa100 lô tường minh (kể cả quá hạn), chỉ dùng các lô đã chọn. Omitted lines giữ FEFO tương thích ADR12. Thiếu tồn trả409/current sau rollback; P05 vẫn422. If-Match vẫn cần đúng cú pháp; cooked replay xét trước so sánh phiên bản cũ, không trừ lại hoặc đổi actor/time.')
op('Q01','GET',base+'/search','Tìm thực phẩm, công thức và lô trong không gian',ref('SearchPage'),uc=['UC20'],queries=page_queries+[par('q',st(maxLength=100)),par('categoryId',array(uuid)),par('location',array(location)),par('expiryState',array(enum('fresh','expiring','expired')))],description='Tìm literal không dấu/case-insensitive; OR trong từng bộ lọc, AND giữa các bộ lọc. categoryId áp dụng food/lot; location/expiryState chỉ áp dụng lot thực tế; công thức bị loại khi có bộ lọc này. facets và filterFacets tính toàn tập đã lọc, trước cursor/limit. Sắp xếp name/type/id ASC. Xem ADR09 trong backend search plan.')
op('Q02','GET',base+'/foods/suggest','Gợi ý tên thực phẩm khi gõ',ref('Search'),uc=['UC11','UC20'],queries=[par('q',st(minLength=2,maxLength=100),required=True),par('limit',integer(minimum=1,maximum=10),10)])
q03=op('Q03','GET',prefix+'/catalog','Danh mục chung và đơn vị đo',ref('Catalog'),uc=['UC11','UC20']);q03['parameters'].append({'$ref':'#/components/parameters/IfNoneMatch'});q03['responses']['304']={'description':'ETag không đổi; không có body.'}
for code,kind,summary in [('B01','spending','Báo cáo chi tiêu'),('B02','consumption','Báo cáo tiêu thụ'),('B03','waste','Báo cáo lãng phí')]:op(code,'GET',base+'/reports/'+kind,summary,ref('Report'),uc=['UC21'],queries=[par('from',date),par('to',date),par('groupBy',enum('day','week','month'),'day')],description='Ngày Việt Nam, từ đầu ngày from đến hết ngày to; tối đa12 tháng. previous có cùng số ngày, change là chênh lệch tuyệt đối. Lô không giá chỉ tính lượng và unpricedLots.')
op('B04','POST',base+'/reports/exports','Yêu cầu xuất báo cáo bất đồng bộ',ref('Export'),ref('ExportInput'),202,['UC22'])
op('B05','GET',base+'/reports/exports/{exportId}','Trạng thái và URL tải báo cáo',ref('Export'),uc=['UC22'])
op('Y03','POST',prefix+'/media/upload-urls','Cấp URL tải ảnh riêng tư',ref('MediaGrant'),ref('MediaInput'),201,['UC04','UC11','UC15'])
op('Y04','POST',prefix+'/media/{objectId}/complete','Xác nhận ảnh, kiểm nội dung và chuyển bất biến',obj({'objectId':uuid,'status':enum('ready')}),uc=['UC04','UC11','UC15'],description='Cần Idempotency-Key. Thao tác đối tượng có tính lũy đẳng tự nhiên; I/O S3 ở ngoài giao dịch nghiệp vụ dài.')
op('HELPER-MEDIA-READ','GET',prefix+'/media/{objectId}','URL tải ảnh đã kiểm quyền',ref('Download'),uc=['UC04','UC11','UC15'])
# Administration. Raw catalog list row names intentionally match to_jsonb(database rows).
op('D01','GET',prefix+'/admin/users','Tra cứu tài khoản',obj({'items':array(ref('AdminUser'))}),uc=['UC24'],queries=page_queries+[par('q',text)])
op('D02','PATCH',prefix+'/admin/users/{id}','Khóa, mở khóa hoặc đổi quyền quản trị',obj({'id':uuid,'version':version}),ref('AdminUserPatch'),uc=['UC24'],match=True)
for kind,code_list,code_edit,inp,out in [('foods','D03','D04','FoodInput','AdminFood'),('food-categories','D05','D05','CategoryInput','AdminCategory'),('units','D06','D06','UnitInput','AdminUnit')]:
 op(code_list,'GET',prefix+'/admin/'+kind,'Xem danh mục '+kind,obj({'items':array(ref(out))}),uc=['UC23'],queries=page_queries)
 op(code_list,'POST',prefix+'/admin/'+kind,'Thêm danh mục '+kind,obj({'id':uuid,'version':integer(minimum=0)}),ref(inp),201,['UC23'])
 op(code_edit,'PUT',prefix+'/admin/'+kind+'/{id}','Cập nhật danh mục '+kind,obj({'id':uuid,'version':integer(minimum=0)}),ref(inp),uc=['UC23'],match=kind=='foods')
 op(code_edit,'DELETE',prefix+'/admin/'+kind+'/{id}','Xóa hoặc ngừng danh mục '+kind,obj({'id':uuid,'version':integer(minimum=0)}),uc=['UC23'])
for method,path,req,resp,status in [('GET','',None,obj({'items':array(ref('Recipe'))}),200),('POST','',ref('TemplateInput'),ref('Recipe'),201),('PUT','/{id}',ref('TemplateInput'),ref('Recipe'),200),('DELETE','/{id}',None,flag('deleted'),200)]:op('D07',method,prefix+'/admin/recipe-templates'+path,'Quản lý công thức mẫu',resp,req,status,['UC23'],match=method=='PUT',queries=page_queries if method=='GET' else ())
op('D08','GET',prefix+'/admin/configs','Xem tham số hệ thống',obj({'items':array(ref('Config'))}),uc=['UC25'])
op('D09','PUT',prefix+'/admin/configs','Cập nhật tham số nguyên tử',obj({'updated':integer(minimum=1)}),ref('ConfigInput'),uc=['UC25'])
op('D10','GET',prefix+'/admin/audit-logs','Xem nhật ký quản trị',obj({'items':array(ref('Audit'))}),uc=['UC24','UC25'],queries=page_queries)
op('D11','POST',prefix+'/admin/foods/{id}/review','Duyệt hoặc gộp thực phẩm riêng',obj({'id':uuid,'action':enum('approve','merge')}),obj({'action':enum('approve','merge'),'targetFoodId':uuid},['action']),uc=['UC23'])
# Operational and contract helpers.
op('OPS-HEALTH','GET','/healthz','Kiểm tra tiến trình',obj({'status':enum('ok')}),public=True)
op('OPS-READY','GET','/readyz','Kiểm tra PostgreSQL và trạng thái limiter',obj({'status':enum('ok','degraded')}),public=True)
doc={'openapi':'3.1.2','jsonSchemaDialect':'https://json-schema.org/draft/2020-12/schema','info':{'title':'Đi Chợ – Backend API','version':'1.0.0','description':'Hợp đồng của backend hiện hành. Bao phủ 86 mã API đang trong phạm vi; Y01/Y02 là định hướng, P08 đã bỏ. Thời gian nghiệp vụ theo Asia/Ho_Chi_Minh. Thành viên ngoài không gian nhận404; quản trị không được đọc dữ liệu nhóm qua quyền quản trị. Lượng fixed-point6, hệ số4, lượng nhập API tối đa2 chữ số; tiền VND số nguyên.'},'servers':[{'url':'/'}],'security':[{'BearerAuth':[]}],'tags':[{'name':k,'description':v} for k,v in {'A':'Tài khoản và thông báo','G':'Không gian và nhóm','S':'Mua sắm','P':'Tủ lạnh','R':'Công thức','M':'Kế hoạch bữa ăn','Q':'Tìm kiếm và danh mục','B':'Báo cáo và xuất tệp','Y':'Tải ảnh','D':'Quản trị','AI':'Gợi ý AI hỗ trợ UC18','Vận hành':'Sức khỏe và trợ giúp'}.items()],'paths':P,'components':{'securitySchemes':{'BearerAuth':{'type':'http','scheme':'bearer','bearerFormat':'JWT','description':'JWT EdDSA, phiên được kiểm tra lại từ PostgreSQL.'}},'parameters':params,'schemas':S,'responses':{'Error':{'description':'Lỗi có mã nghiệp vụ; 429 hoặc lỗi bận có thể kèm Retry-After.','headers':{'Retry-After':{'schema':st()},'X-Request-ID':{'schema':uuid},'X-Trace-ID':{'schema':traceid}},'content':{'application/json':{'schema':ref('Error'),'example':{'code':4001,'message':'Không tìm thấy dữ liệu'}}}}}},'x-api-coverage':{'specifiedCodes':88,'implementedCodes':86,'deferredCodes':['Y01','Y02'],'removedCodes':['P08'],'extensionCodes':['AI01','AI02'],'source':'chapters/c3/api.tex; backmatter/pl/f-dac-ta-api.tex; current Go registrations'},'x-implementation-notes':['Thông báo trả data.items cùng meta.nextCursor và meta.serverTime; shopping dùng data.lists, pantry dùng data.lots và data.nextCursor.','GET danh mục có304 không body. Pantry/shopping/inbox/recipe/meal/admin collections có cursor default20/max100; search có cursor và full-filter facets; autocomplete giữ tối đa10.','Các API xác nhận dryRun miễn Idempotency-Key khi chỉ xem trước.','Các mutation cần quyền đang kiểm lại membership/admin trong dịch vụ và transaction.']}
S['A17_getResponse']['properties']['meta']=obj({'nextCursor':text,'serverTime':timestamp})
put('PageMeta',obj({'nextCursor':text,'serverTime':timestamp}))
for name in ['P01_getResponse','P03_getResponse','P07_getResponse','S01_getResponse','R01_getResponse','M01_getResponse','D01_getResponse','D03_getResponse','D05_getResponse','D06_getResponse','D07_getResponse','D10_getResponse','Q01_getResponse']:
 S[name]['properties']['meta']=ref('PageMeta');S[name]['required'].append('meta')
put('ClientResourceID',{'anyOf':[st(format='uuid',pattern=r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$'),{'const':''},{'type':'null'}],'description':'ID do client sinh phải là UUIDv7. Bỏ qua, null hoặc chuỗi rỗng để máy chủ sinh mới; ID đã tồn tại nhận409/4003.'})
for name in ['LotInput','ListInput','ItemInput','RecipeInput','TemplateInput','MealInput']:
 S[name]['properties']['id']=ref('ClientResourceID')
copy_input=P[base+'/shopping-lists/{listId}/copy']['post']['requestBody']['content']['application/json']['schema']
copy_input['properties']['id']=ref('ClientResourceID')
copy_input['properties']['notes']=nullable(st(maxLength=200,description='Bỏ qua hoặc null để giữ ghi chú nguồn; chuỗi rỗng để xóa.'))

S['A17_getResponse']['required'].append('meta')
# The static document is a plain OpenAPI object, not a success envelope.
P['/openapi.json']={'get':{'operationId':'OPS_OPENAPI_get','summary':'Tải hợp đồng OpenAPI 3.1','tags':['Vận hành'],'x-api-code':'OPS-OPENAPI','x-use-case':[],'security':[],'responses':{'200':{'description':'Tài liệu OpenAPI JSON','content':{'application/json':{'schema':{'type':'object','required':['openapi','info','paths','components'],'properties':{'openapi':{'const':'3.1.2'},'info':{'type':'object'},'paths':{'type':'object'},'components':{'type':'object'}},'additionalProperties':True}}}}}}}
pathlib.Path(__file__).with_name('openapi.json').write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n')
print(f'{len(P)} paths, {sum(len(p) for p in P.values())} operations, {len(S)} schemas')
